const BASE85_ALPHABET = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!#$%&()*+-;<=>?@^_`{|}~'
export const SECURE_INVITE_LENGTH = 12

// 12 base85 characters carry about 77 bits of entropy. Rejection sampling
// keeps every alphabet character equally likely.
export function createSecureInvite(): string {
  let invite = ''
  const random = new Uint8Array(32)
  while (invite.length < SECURE_INVITE_LENGTH) {
    crypto.getRandomValues(random)
    for (const value of random) {
      if (value === 255) continue
      invite += BASE85_ALPHABET[value % BASE85_ALPHABET.length]
      if (invite.length === SECURE_INVITE_LENGTH) return invite
    }
  }
  return invite
}

export function isSecureInvite(value: string): boolean {
  return value.length === SECURE_INVITE_LENGTH && Array.from(value).every((character) => BASE85_ALPHABET.includes(character))
}
