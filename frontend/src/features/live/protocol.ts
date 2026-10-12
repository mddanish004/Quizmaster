export type Envelope = {
  v: 1
  type: string
  eventId?: string
  sessionId: string
  stateVersion?: number
  sentAt: string
  requestId?: string | null
  expectedVersion?: number
  payload: Record<string, unknown>
}

export type LiveState = 'lobby' | 'question_open' | 'question_results' | 'leaderboard' | 'ended'

export type Snapshot = {
  sessionId: string
  state: LiveState
  stateVersion: number
  serverTime: string
  participantCount: number
  questionCount: number
  joinCode?: string
  question?: { id: string; position: number; prompt: string; options: { id: string; text: string; position: number }[] } | null
  questionStartedAt?: string | null
  questionEndsAt?: string | null
  participant?: { id: string; displayName: string; score: number }
}

export function wsUrl(sessionId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws/v1/sessions/${sessionId}`
}

export function uuid(): string {
  return crypto.randomUUID()
}
