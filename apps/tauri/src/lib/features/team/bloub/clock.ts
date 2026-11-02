/** One animation frame loop shared by every live Team avatar. */
type Subscriber = (seconds: number) => void;

const subscribers = new Set<Subscriber>();
let frame = 0;
let startedAt = 0;

function tick(timestamp: number): void {
  if (!subscribers.size) {
    frame = 0;
    startedAt = 0;
    return;
  }
  if (!startedAt) startedAt = timestamp;
  const seconds = (timestamp - startedAt) / 1000;
  for (const subscriber of subscribers) subscriber(seconds);
  frame = requestAnimationFrame(tick);
}

export function subscribeBloubClock(subscriber: Subscriber): () => void {
  subscribers.add(subscriber);
  subscriber(0);
  if (!frame) frame = requestAnimationFrame(tick);
  return () => {
    subscribers.delete(subscriber);
    if (!subscribers.size && frame) {
      cancelAnimationFrame(frame);
      frame = 0;
      startedAt = 0;
    }
  };
}
