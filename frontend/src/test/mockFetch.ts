// mockFetch builds minimal fetch Response doubles that match what src/lib/api.ts
// consumes: `text()` for all responses (empty string = empty body).
export function jsonResponse(payload: unknown, status = 200, ok = true) {
  const text = JSON.stringify(payload)
  return { ok, status, text: async () => text, json: async () => payload }
}

export function emptyResponse(status = 202) {
  return { ok: true, status, text: async () => '', json: async () => { throw new Error('empty body') } }
}
