// Quadratic corner fillets keep the baked outlines as soft as the Original shapes.
export function rounded(points, radius = .2) {
  return points.flatMap((point, i) => {
    const previous = points[(i + points.length - 1) % points.length]
    const next = points[(i + 1) % points.length]
    const a = Math.hypot(previous[0] - point[0], previous[1] - point[1])
    const b = Math.hypot(next[0] - point[0], next[1] - point[1])
    const cut = Math.min(radius, a / 2, b / 2)
    const start = point.map((v, j) => v + (previous[j] - v) * cut / a)
    const end = point.map((v, j) => v + (next[j] - v) * cut / b)
    return Array.from({length: 9}, (_, step) => {
      const t = step / 8
      return point.map((v, j) => (1-t)**2 * start[j] + 2*t*(1-t)*v + t*t*end[j])
    })
  })
}
