/**
 * Reed-Solomon decoder compatible with klauspost/reedsolomon (Go).
 *
 * Encoding matrix (Vandermonde):
 * - Rows 0..k-1: identity matrix (data shards)
 * - Rows k..k+m-1: Vandermonde rows, row k+i has element j = (i+1)^j
 *   where exponentiation is in GF(256)
 *
 * Decoding: pick any k available rows, invert the k×k submatrix,
 * multiply by available shards to recover original data.
 */

// --- GF(256) arithmetic with primitive polynomial 0x11d ---

const GF_EXP = new Uint8Array(512)
const GF_LOG = new Uint8Array(256)

;(function init() {
  let x = 1
  for (let i = 0; i < 255; i++) {
    GF_EXP[i] = x
    GF_LOG[x] = i
    x <<= 1
    if (x >= 256) x ^= 0x11d
  }
  for (let i = 255; i < 512; i++) {
    GF_EXP[i] = GF_EXP[i - 255]
  }
})()

function gfMul(a: number, b: number): number {
  if (a === 0 || b === 0) return 0
  return GF_EXP[GF_LOG[a] + GF_LOG[b]]
}

function gfDiv(a: number, b: number): number {
  if (b === 0) throw new Error('div by zero')
  if (a === 0) return 0
  return GF_EXP[(GF_LOG[a] + 255 - GF_LOG[b]) % 255]
}

function gfInv(a: number): number {
  if (a === 0) throw new Error('inv of zero')
  return GF_EXP[255 - GF_LOG[a]]
}

// --- Matrix operations in GF(256) ---

/**
 * Invert an n×n matrix in GF(256) via Gauss-Jordan elimination.
 */
function gfMatrixInv(matrix: number[][]): number[][] {
  const n = matrix.length
  // Create augmented matrix [M | I]
  const aug: number[][] = matrix.map((row, i) => [
    ...row,
    ...Array.from({ length: n }, (_, j) => (i === j ? 1 : 0))
  ])

  for (let col = 0; col < n; col++) {
    // Find pivot
    let pivot = -1
    for (let row = col; row < n; row++) {
      if (aug[row][col] !== 0) {
        pivot = row
        break
      }
    }
    if (pivot === -1) throw new Error('matrix singular')

    // Swap
    if (pivot !== col) {
      const tmp = aug[col]
      aug[col] = aug[pivot]
      aug[pivot] = tmp
    }

    // Scale pivot row so aug[col][col] = 1
    const invScale = gfInv(aug[col][col])
    for (let j = 0; j < 2 * n; j++) {
      aug[col][j] = gfMul(aug[col][j], invScale)
    }

    // Eliminate all other rows
    for (let row = 0; row < n; row++) {
      if (row === col) continue
      const factor = aug[row][col]
      if (factor === 0) continue
      for (let j = 0; j < 2 * n; j++) {
        aug[row][j] ^= gfMul(factor, aug[col][j])
      }
    }
  }

  return aug.map((row) => row.slice(n))
}

/**
 * Multiply an n×n matrix by an n-element column vector.
 */
function gfMatVec(mat: number[][], vec: number[]): number[] {
  return mat.map((row) => {
    let acc = 0
    for (let i = 0; i < vec.length; i++) {
      acc ^= gfMul(row[i], vec[i])
    }
    return acc
  })
}

/**
 * Get the encoding matrix element for row r, column c
 * using the same scheme as klauspost/reedsolomon.
 * - Data rows (r < k): identity (1 if r==c else 0)
 * - Parity rows (r >= k): gfPow(r-k+1, c) = (r-k+1)^c in GF(256)
 *
 * gfPow(x, n) = exp(log(x) * n mod 255)
 * gfPow(0, n) = 0 for n > 0
 * gfPow(x, 0) = 1 for any x
 */
function gfPow(base: number, exp: number): number {
  if (exp === 0) return 1
  if (base === 0) return 0
  return GF_EXP[(GF_LOG[base] * exp) % 255]
}

function encodingMatrixElement(row: number, col: number, k: number, _m: number): number {
  if (row < k) {
    return row === col ? 1 : 0
  }
  // Parity row: Vandermonde with eval point (row - k + 1)
  const evalPoint = row - k + 1
  return gfPow(evalPoint, col)
}

/**
 * Decode a group of shards using Reed-Solomon.
 *
 * @param available - Map of shardIndex -> shardData for available shards
 * @param k - number of data shards
 * @param shardSize - size of each shard in bytes
 * @returns Concatenated original data bytes (first k shards)
 */
export function rsDecode(
  available: Map<number, Uint8Array>,
  k: number,
  shardSize: number
): Uint8Array {
  const availableIndices = Array.from(available.keys()).slice(0, k)
  if (availableIndices.length < k) {
    throw new Error(`need at least ${k} shards, have ${availableIndices.length}`)
  }

  // Build k×k encoding submatrix from available rows
  const subMatrix: number[][] = availableIndices.map((row) =>
    Array.from({ length: k }, (_, col) => encodingMatrixElement(row, col, k, available.size - k))
  )

  // Invert to get decoding matrix
  const decodeMatrix = gfMatrixInv(subMatrix)

  // Decode each byte position
  const shardDatas = availableIndices.map((idx) => available.get(idx)!)
  const output = new Uint8Array(k * shardSize)

  for (let byteIdx = 0; byteIdx < shardSize; byteIdx++) {
    // Extract this byte from each available shard
    const vals = shardDatas.map((s) => s[byteIdx])
    // Multiply by decode matrix to recover original data bytes
    const decoded = gfMatVec(decodeMatrix, vals)
    for (let i = 0; i < k; i++) {
      output[i * shardSize + byteIdx] = decoded[i]
    }
  }

  return output
}
