let csrfToken = ''

export function setCsrf(token: string) {
  csrfToken = token
}

export function getCsrf() {
  return csrfToken
}
