import { useEffect, useReducer, useRef, useCallback } from 'react'
import { type Envelope, type LiveState, type Snapshot, wsUrl, uuid } from './protocol'

export type LiveView = {
  connected: boolean
  retrying: boolean
  snapshot: Snapshot | null
  lastEvent: Envelope | null
  results: Record<string, unknown> | null
  leaders: { participantId: string; displayName: string; rank: number; score: number }[]
  ack: { submissionId: string; outcome: string } | null
  error: string | null
  serverOffsetMs: number
}

type Action =
  | { type: 'socket'; connected: boolean; retrying: boolean }
  | { type: 'event'; env: Envelope }
  | { type: 'error'; message: string }

const initial: LiveView = {
  connected: false, retrying: false, snapshot: null, lastEvent: null,
  results: null, leaders: [], ack: null, error: null, serverOffsetMs: 0,
}

function reduce(state: LiveView, action: Action): LiveView {
  switch (action.type) {
    case 'socket':
      return { ...state, connected: action.connected, retrying: action.retrying }
    case 'error':
      return { ...state, error: action.message }
    case 'event': {
      const env = action.env
      // Discard stale versions (except acks/errors/pong which carry no version).
      if (env.stateVersion && state.snapshot && env.stateVersion < state.snapshot.stateVersion) {
        return state
      }
      switch (env.type) {
        case 'state_snapshot': {
          const p = env.payload as unknown as Snapshot
          const offset = Date.parse(p.serverTime) - Date.now()
          return { ...state, snapshot: { ...p, stateVersion: env.stateVersion ?? p.stateVersion }, lastEvent: env, serverOffsetMs: offset, error: null }
        }
        case 'question_started':
          return {
            ...state, lastEvent: env, results: null, ack: null,
            snapshot: state.snapshot ? { ...state.snapshot, state: 'question_open' as LiveState, stateVersion: env.stateVersion ?? state.snapshot.stateVersion, question: (env.payload['question'] ?? null) as Snapshot['question'], questionEndsAt: (env.payload['endsAt'] ?? null) as string | null } : state.snapshot,
          }
        case 'answer_acknowledged':
          return { ...state, lastEvent: env, ack: { submissionId: String(env.payload['submissionId']), outcome: String(env.payload['outcome']) } }
        case 'question_results':
          return {
            ...state, lastEvent: env, results: env.payload,
            snapshot: state.snapshot ? { ...state.snapshot, state: 'question_results' as LiveState, stateVersion: env.stateVersion ?? state.snapshot.stateVersion } : state.snapshot,
          }
        case 'leaderboard_updated':
          return {
            ...state, lastEvent: env,
            leaders: ((env.payload['leaders'] ?? []) as LiveView['leaders']),
            snapshot: state.snapshot ? { ...state.snapshot, state: 'leaderboard' as LiveState, stateVersion: env.stateVersion ?? state.snapshot.stateVersion } : state.snapshot,
          }
        case 'session_ended':
          return {
            ...state, lastEvent: env,
            snapshot: state.snapshot ? { ...state.snapshot, state: 'ended' as LiveState, stateVersion: env.stateVersion ?? state.snapshot.stateVersion } : state.snapshot,
          }
        case 'question_ended':
          return { ...state, lastEvent: env }
        case 'participant_count_changed':
          return { ...state, lastEvent: env, snapshot: state.snapshot ? { ...state.snapshot, participantCount: Number(env.payload['participantCount'] ?? 0) } : state.snapshot }
        case 'answer_submitted':
          return { ...state, lastEvent: env }
        case 'participant_left':
          // Presence only (answers/scores persist); host roster shows counts.
          return { ...state, lastEvent: env }
        case 'session_error':
          return { ...state, lastEvent: env, error: String(env.payload['message'] ?? 'Session error') }
        case 'server_restart':
          return { ...state, retrying: true }
        case 'pong':
          return { ...state, serverOffsetMs: Date.parse(String(env.payload['serverTime'])) - Date.now() }
        default:
          return { ...state, lastEvent: env }
      }
    }
  }
}

// useLiveSocket connects with exponential backoff + full jitter and resyncs
// from the last applied version. Auth rides the HttpOnly cookies.
export function useLiveSocket(sessionId: string) {
  const [view, dispatch] = useReducer(reduce, initial)
  const ws = useRef<WebSocket | null>(null)
  const version = useRef(0)
  const stableSince = useRef(Date.now())
  const closedByUs = useRef(false)

  const send = useCallback((type: string, payload: Record<string, unknown>, expectedVersion?: number) => {
    const env: Envelope = {
      v: 1, type, sessionId, sentAt: new Date().toISOString(),
      requestId: uuid(), payload, expectedVersion,
    }
    const socket = ws.current
    socket?.send(JSON.stringify(env))
    return env.requestId as string
  }, [sessionId])

  useEffect(() => {
    version.current = view.snapshot?.stateVersion ?? version.current
  }, [view.snapshot])

  useEffect(() => {
    closedByUs.current = false
    let attempt = 0
    let timer: ReturnType<typeof setTimeout> | null = null
    let dead = false

    const connect = () => {
      if (dead) return
      dispatch({ type: 'socket', connected: false, retrying: attempt > 0 })
      const url = wsUrl(sessionId) + (version.current > 0 ? `?lastVersion=${version.current}` : '')
      let socket: WebSocket
      try {
        socket = new WebSocket(url)
      } catch {
        schedule()
        return
      }
      ws.current = socket
      socket.onopen = () => {
        attempt = 0
        stableSince.current = Date.now()
        dispatch({ type: 'socket', connected: true, retrying: false })
      }
      socket.onmessage = (ev) => {
        try {
          const env = JSON.parse(String(ev.data)) as Envelope
          if (env.stateVersion && env.stateVersion > version.current) version.current = env.stateVersion
          dispatch({ type: 'event', env })
        } catch {
          dispatch({ type: 'error', message: 'Received an unreadable message.' })
        }
      }
      socket.onclose = () => {
        // Only release the ref if this socket is still current: a stale
        // socket closing late must never drop a newer connection (StrictMode
        // remounts and reconnect overlap hit this path).
        if (ws.current === socket) ws.current = null
        dispatch({ type: 'socket', connected: false, retrying: true })
        if (!closedByUs.current) schedule()
      };
      socket.onerror = () => {
        socket.close()
      }
    }
    const schedule = () => {
      // 0.5, 1, 2, 4, 8s capped at 10, full jitter.
      const base = Math.min(500 * 2 ** attempt, 10000)
      const delay = Math.random() * base
      attempt++
      timer = setTimeout(connect, delay)
    }
    connect()
    return () => {
      dead = true
      closedByUs.current = true
      if (timer) clearTimeout(timer)
      ws.current?.close()
      ws.current = null
    }
  }, [sessionId])

  return { view, send }
}
