// Shared QR Code encoder per ISO/IEC 18004, byte mode only, EC levels L and M.
// Self-contained: no dependencies and no Node APIs, so it runs in the renderer.

type EcLevel = 'L' | 'M';

const MAX_VERSION = 40;

// Table 9: EC codewords per block, and block count, indexed by version - 1.
// The spec assigns these; they are not derivable from the geometry.
const EC_PER_BLOCK: Record<EcLevel, readonly number[]> = {
  L: [7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28,
    28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  M: [10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26,
    26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28],
};

const BLOCK_COUNT: Record<EcLevel, readonly number[]> = {
  L: [1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8,
    8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25],
  M: [1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16,
    17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49],
};

// Table 12: EC level indicator carried in the format information.
const EC_INDICATOR: Record<EcLevel, number> = {L: 1, M: 0};

// Table 10 mask condition per pattern reference, applied to unreserved modules.
const MASKS: readonly ((row: number, col: number) => boolean)[] = [
  (row, col) => (row + col) % 2 === 0,
  (row) => row % 2 === 0,
  (_row, col) => col % 3 === 0,
  (row, col) => (row + col) % 3 === 0,
  (row, col) => (Math.floor(row / 2) + Math.floor(col / 3)) % 2 === 0,
  (row, col) => row * col % 2 + row * col % 3 === 0,
  (row, col) => (row * col % 2 + row * col % 3) % 2 === 0,
  (row, col) => ((row + col) % 2 + row * col % 3) % 2 === 0,
];

// Table 11 penalty weights for the four mask evaluation rules.
const N1 = 3;
const N2 = 3;
const N3 = 40;
const N4 = 10;

// GF(256) modulo x^8 + x^4 + x^3 + x^2 + 1 with 2 as generator. EXP is doubled so
// LOG sums can be indexed without wrapping.
const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);

for (let i = 0, x = 1; i < 255; i++) {
  GF_EXP[i] = x;
  GF_LOG[x] = i;
  x = x << 1 ^ (x & 0x80 ? 0x11d : 0);
}
for (let i = 255; i < 512; i++) GF_EXP[i] = GF_EXP[i - 255];

function moduleCount(version: number): number {
  return version * 4 + 17;
}

/** Alignment pattern centre coordinates, ascending (Annex E). */
function alignmentPositions(version: number): number[] {
  if (version === 1) return [];
  const count = Math.floor(version / 7) + 2;
  // Spacing is even-valued and as uniform as possible; version 32 is the single
  // case the general expression does not reproduce.
  const step = version === 32
    ? 26
    : Math.floor((version * 4 + count * 2 + 1) / (count * 2 - 2)) * 2;
  const positions = [6];
  for (let pos = version * 4 + 10; positions.length < count; pos -= step) positions.splice(1, 0, pos);
  return positions;
}

/** Modules left for data and EC once every function pattern is subtracted. */
function rawDataModules(version: number): number {
  const size = moduleCount(version);
  let count = size * size;
  count -= 3 * 8 * 8;              // finder patterns including separators
  count -= 2 * (size - 16);        // timing patterns
  count -= 31;                     // format information plus the dark module
  if (version >= 7) count -= 2 * 18;
  const centres = alignmentPositions(version).length;
  if (centres > 0) {
    // Three centres are absorbed by the finders, and each centre sharing row or
    // column 6 overlaps the timing pattern by five modules.
    count -= 25 * (centres * centres - 3);
    count += 10 * (centres - 2);
  }
  return count;
}

function totalCodewords(version: number): number {
  return Math.floor(rawDataModules(version) / 8);
}

function dataCodewords(version: number, level: EcLevel): number {
  return totalCodewords(version) - EC_PER_BLOCK[level][version - 1] * BLOCK_COUNT[level][version - 1];
}

/** Byte mode character count indicator width (Table 3). */
function charCountBits(version: number): number {
  return version <= 9 ? 8 : 16;
}

function appendBits(bits: number[], value: number, length: number): void {
  for (let i = length - 1; i >= 0; i--) bits.push(value >>> i & 1);
}

function smallestVersion(byteLength: number, level: EcLevel): number | null {
  for (let version = 1; version <= MAX_VERSION; version++) {
    const needed = 4 + charCountBits(version) + byteLength * 8;
    if (needed <= dataCodewords(version, level) * 8) return version;
  }
  return null;
}

/** Mode indicator, length, payload, terminator and pad codewords (8.4). */
function buildDataCodewords(bytes: Uint8Array, version: number, level: EcLevel): number[] {
  const capacity = dataCodewords(version, level);
  const bits: number[] = [];
  appendBits(bits, 0b0100, 4);
  appendBits(bits, bytes.length, charCountBits(version));
  for (const byte of bytes) appendBits(bits, byte, 8);
  appendBits(bits, 0, Math.min(4, capacity * 8 - bits.length));
  appendBits(bits, 0, (8 - bits.length % 8) % 8);
  const codewords: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let word = 0;
    for (let j = 0; j < 8; j++) word = word << 1 | bits[i + j];
    codewords.push(word);
  }
  // Pad codewords alternate from 0xEC (8.4.9).
  for (let i = 0; codewords.length < capacity; i++) codewords.push(i % 2 === 0 ? 0xec : 0x11);
  return codewords;
}

function gfMultiply(a: number, b: number): number {
  if (a === 0 || b === 0) return 0;
  return GF_EXP[GF_LOG[a] + GF_LOG[b]];
}

/** Product of (x - a^i) for i < degree, leading coefficient first. */
function generatorPoly(degree: number): number[] {
  let poly = [1];
  for (let i = 0; i < degree; i++) {
    const next = new Array<number>(poly.length + 1).fill(0);
    for (let j = 0; j < poly.length; j++) {
      next[j] ^= poly[j];
      next[j + 1] ^= gfMultiply(poly[j], GF_EXP[i]);
    }
    poly = next;
  }
  return poly;
}

/** Reed-Solomon remainder of one block. */
function ecCodewords(block: number[], degree: number): number[] {
  const generator = generatorPoly(degree);
  const remainder = new Array<number>(degree).fill(0);
  for (const byte of block) {
    const factor = byte ^ remainder[0];
    remainder.shift();
    remainder.push(0);
    for (let i = 0; i < degree; i++) remainder[i] ^= gfMultiply(generator[i + 1], factor);
  }
  return remainder;
}

/** Split into blocks, append EC, then interleave both groups (8.6). */
function interleave(data: number[], version: number, level: EcLevel): number[] {
  const blocks = BLOCK_COUNT[level][version - 1];
  const ecLength = EC_PER_BLOCK[level][version - 1];
  const shortLength = Math.floor(data.length / blocks);
  const shortCount = blocks - data.length % blocks;
  const dataBlocks: number[][] = [];
  const ecBlocks: number[][] = [];
  for (let i = 0, offset = 0; i < blocks; i++) {
    const length = shortLength + (i < shortCount ? 0 : 1);
    const block = data.slice(offset, offset + length);
    offset += length;
    dataBlocks.push(block);
    ecBlocks.push(ecCodewords(block, ecLength));
  }
  const result: number[] = [];
  for (let i = 0; i <= shortLength; i++)
    for (const block of dataBlocks) if (i < block.length) result.push(block[i]);
  for (let i = 0; i < ecLength; i++)
    for (const block of ecBlocks) result.push(block[i]);
  return result;
}

function emptyMatrix(size: number): boolean[][] {
  return Array.from({length: size}, () => new Array<boolean>(size).fill(false));
}

/** 7x7 concentric squares plus the one-module separator, clipped to the grid. */
function drawFinder(modules: boolean[][], reserved: boolean[][], row: number, col: number): void {
  const size = modules.length;
  for (let dr = -1; dr <= 7; dr++)
    for (let dc = -1; dc <= 7; dc++) {
      const r = row + dr;
      const c = col + dc;
      if (r < 0 || r >= size || c < 0 || c >= size) continue;
      const ring = Math.max(Math.abs(dr - 3), Math.abs(dc - 3));
      modules[r][c] = ring <= 3 && ring !== 2;
      reserved[r][c] = true;
    }
}

function drawFunctionPatterns(modules: boolean[][], reserved: boolean[][], version: number): void {
  const size = modules.length;

  for (let i = 0; i < size; i++) {
    modules[6][i] = i % 2 === 0;
    modules[i][6] = i % 2 === 0;
    reserved[6][i] = true;
    reserved[i][6] = true;
  }

  const positions = alignmentPositions(version);
  const last = positions[positions.length - 1];
  for (const row of positions)
    for (const col of positions) {
      // The three centres coinciding with finder patterns carry no alignment pattern.
      if (row === 6 && (col === 6 || col === last) || row === last && col === 6) continue;
      for (let dr = -2; dr <= 2; dr++)
        for (let dc = -2; dc <= 2; dc++) {
          modules[row + dr][col + dc] = Math.max(Math.abs(dr), Math.abs(dc)) !== 1;
          reserved[row + dr][col + dc] = true;
        }
    }

  drawFinder(modules, reserved, 0, 0);
  drawFinder(modules, reserved, size - 7, 0);
  drawFinder(modules, reserved, 0, size - 7);

  // Reserve the format information strips and the dark module.
  for (let i = 0; i < 9; i++) {
    reserved[8][i] = true;
    reserved[i][8] = true;
  }
  for (let i = 0; i < 8; i++) {
    reserved[8][size - 1 - i] = true;
    reserved[size - 1 - i][8] = true;
  }
  modules[size - 8][8] = true;

  if (version >= 7)
    for (let i = 0; i < 18; i++) {
      const a = size - 11 + i % 3;
      const b = Math.floor(i / 3);
      reserved[b][a] = true;
      reserved[a][b] = true;
    }
}

/** Two-module columns walked right to left, alternating up and down (8.7.3). */
function placeCodewords(modules: boolean[][], reserved: boolean[][], codewords: number[]): void {
  const size = modules.length;
  const total = codewords.length * 8;
  let bit = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5; // column 6 is the vertical timing pattern
    for (let vertical = 0; vertical < size; vertical++)
      for (let j = 0; j < 2; j++) {
        const col = right - j;
        const upward = (right + 1 & 2) === 0;
        const row = upward ? size - 1 - vertical : vertical;
        if (reserved[row][col] || bit >= total) continue; // remainder bits stay light
        modules[row][col] = (codewords[bit >>> 3] >>> (7 - (bit & 7)) & 1) === 1;
        bit++;
      }
  }
}

/** 15-bit format information: BCH(15,5) remainder, then the 101010000010010 mask (Annex C). */
function formatBits(level: EcLevel, mask: number): number {
  const data = EC_INDICATOR[level] << 3 | mask;
  let remainder = data << 10;
  for (let i = 14; i >= 10; i--)
    if ((remainder >>> i & 1) === 1) remainder ^= 0x537 << (i - 10);
  return (data << 10 | remainder) ^ 0x5412;
}

/** 18-bit version information: BCH(18,6) remainder (Annex D). */
function versionInfoBits(version: number): number {
  let remainder = version << 12;
  for (let i = 17; i >= 12; i--)
    if ((remainder >>> i & 1) === 1) remainder ^= 0x1f25 << (i - 12);
  return version << 12 | remainder;
}

function drawFormatInfo(modules: boolean[][], level: EcLevel, mask: number): void {
  const size = modules.length;
  const bits = formatBits(level, mask);
  const at = (index: number) => (bits >>> index & 1) === 1;

  for (let i = 0; i <= 5; i++) modules[i][8] = at(i);
  modules[7][8] = at(6);
  modules[8][8] = at(7);
  modules[8][7] = at(8);
  for (let i = 9; i < 15; i++) modules[8][14 - i] = at(i);

  for (let i = 0; i < 8; i++) modules[8][size - 1 - i] = at(i);
  for (let i = 8; i < 15; i++) modules[size - 15 + i][8] = at(i);
}

function drawVersionInfo(modules: boolean[][], version: number): void {
  if (version < 7) return;
  const size = modules.length;
  const bits = versionInfoBits(version);
  for (let i = 0; i < 18; i++) {
    const dark = (bits >>> i & 1) === 1;
    const a = size - 11 + i % 3;
    const b = Math.floor(i / 3);
    modules[b][a] = dark;
    modules[a][b] = dark;
  }
}

/** Penalty rules 1 and 3 over a single row or column. */
function linePenalty(line: boolean[]): number {
  let score = 0;
  for (let start = 0, i = 1; i <= line.length; i++) {
    if (i < line.length && line[i] === line[start]) continue;
    const run = i - start;
    if (run >= 5) score += N1 + run - 5;
    start = i;
  }
  // 1:1:3:1:1 ratio bounded by four light modules on either side; the grid edge
  // counts as light, and a pattern clear on both sides scores twice.
  const light = (index: number) => index < 0 || index >= line.length || !line[index];
  for (let i = 0; i + 7 <= line.length; i++) {
    if (!(line[i] && light(i + 1) && line[i + 2] && line[i + 3] && line[i + 4] && light(i + 5) && line[i + 6]))
      continue;
    if (light(i - 1) && light(i - 2) && light(i - 3) && light(i - 4)) score += N3;
    if (light(i + 7) && light(i + 8) && light(i + 9) && light(i + 10)) score += N3;
  }
  return score;
}

function penalty(modules: boolean[][]): number {
  const size = modules.length;
  let score = 0;

  for (let i = 0; i < size; i++) {
    score += linePenalty(modules[i]);
    score += linePenalty(modules.map((row) => row[i]));
  }

  // Rule 2: every 2x2 block of a single colour.
  for (let r = 0; r + 1 < size; r++)
    for (let c = 0; c + 1 < size; c++) {
      const module = modules[r][c];
      if (module === modules[r][c + 1] && module === modules[r + 1][c] && module === modules[r + 1][c + 1])
        score += N2;
    }

  // Rule 4: deviation of the dark module proportion from 50%, in 5% steps.
  let dark = 0;
  for (const row of modules) for (const module of row) if (module) dark++;
  score += N4 * Math.floor(Math.abs(dark * 100 / (size * size) - 50) / 5);

  return score;
}

/** Copy of `base` with one mask applied to the unreserved modules, plus format info. */
function applyMask(base: boolean[][], reserved: boolean[][], mask: number, level: EcLevel): boolean[][] {
  const condition = MASKS[mask];
  const masked = base.map((row, r) => row.map((module, c) => module !== (!reserved[r][c] && condition(r, c))));
  drawFormatInfo(masked, level, mask);
  return masked;
}

function render(codewords: number[], version: number, level: EcLevel): boolean[][] {
  const size = moduleCount(version);
  const reserved = emptyMatrix(size);
  const base = emptyMatrix(size);
  drawFunctionPatterns(base, reserved, version);
  drawVersionInfo(base, version);
  placeCodewords(base, reserved, codewords);

  let bestMask = 0;
  let bestScore = Infinity;
  for (let mask = 0; mask < MASKS.length; mask++) {
    const score = penalty(applyMask(base, reserved, mask, level));
    if (score < bestScore) {
      bestScore = score;
      bestMask = mask;
    }
  }
  return applyMask(base, reserved, bestMask, level);
}

/**
 * Encodes `text` as a QR symbol and returns its modules, true meaning dark.
 * The matrix carries no quiet zone; callers add the four-module border.
 *
 * Level M is preferred at the smallest version that fits, because these symbols
 * are read off a screen where the extra redundancy is cheap. Payloads too large
 * for M at version 40 fall back to level L, which reaches 2953 bytes.
 */
export function qrMatrix(text: string): boolean[][] {
  const bytes = new TextEncoder().encode(text);
  const level: EcLevel = smallestVersion(bytes.length, 'M') === null ? 'L' : 'M';
  const version = smallestVersion(bytes.length, level);
  if (version === null)
    throw new Error(`Cannot encode ${bytes.length} bytes as a QR code; the limit is ${dataCodewords(MAX_VERSION, 'L') - 3} bytes.`);
  return render(interleave(buildDataCodewords(bytes, version, level), version, level), version, level);
}

/**
 * Encodes `text` and returns its module count alongside an SVG path drawing every
 * dark module, for `<svg viewBox="0 0 size size"><path d={path}/></svg>`.
 * Horizontally adjacent modules share one subpath to keep the string small.
 */
export function qrSvgPath(text: string): {size: number; path: string} {
  const modules = qrMatrix(text);
  const parts: string[] = [];
  for (let r = 0; r < modules.length; r++)
    for (let c = 0; c < modules.length; c++) {
      if (!modules[r][c]) continue;
      const start = c;
      while (c + 1 < modules.length && modules[r][c + 1]) c++;
      const run = c + 1 - start;
      parts.push(`M${start},${r}h${run}v1h-${run}z`);
    }
  return {size: modules.length, path: parts.join('')};
}
