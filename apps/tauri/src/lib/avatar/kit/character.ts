import { commandsPath } from './solid';
import { assetCache } from './assetCache';

type Part = { path: Path2D; color: string; front: boolean; surface: boolean; alpha: number };
export type Character = { body: Path2D; color: string; eyeColor: string; eyes: Path2D[]; parts: Part[] };
type Source = { body: number[][]; color: string; eyeColor: string; eyes: number[][][];
  parts: { path: number[][]; color: string; front: boolean; surface: boolean; alpha: number }[] };

const files = import.meta.glob<{ default: Record<string, Source> }>('./*.json');
const sources: Record<string, () => Promise<Character>> = {};
for (const [path, load] of Object.entries(files)) {
  const id = path.slice(2, -5);
  if (id === 'shapes' || id === 'plus') continue;
  sources[id] = async () => {
    const source = (await load()).default[id];
    if (!source) throw new Error('Missing avatar geometry');
    return {
      body: commandsPath([source.body]),
      color: source.color,
      eyeColor: source.eyeColor,
      eyes: source.eyes.map((eye) => commandsPath([eye])),
      parts: source.parts.map((part) => ({ ...part, path: commandsPath([part.path]) })),
    };
  };
}
const cache = assetCache(sources);

/** Loaded neutral reference geometry is shared across all views of the same character. */
export const hasCharacter = cache.has;
export const characterPaths = cache.get;
export const loadCharacter = cache.load;

/** Composites the same back, body, front and eye layers as AvatarLayers. */
export function drawCharacter(context: CanvasRenderingContext2D, character: Character): void {
  const part = (value: Part) => {
    context.save();
    if (value.surface) context.clip(character.body);
    context.globalAlpha = value.alpha;
    context.fillStyle = value.color;
    context.fill(value.path);
    context.restore();
  };
  character.parts.filter((value) => !value.front).forEach(part);
  context.fillStyle = character.color;
  context.fill(character.body);
  character.parts.filter((value) => value.front).forEach(part);
  context.save();
  context.clip(character.body);
  context.fillStyle = character.eyeColor;
  for (const eye of character.eyes) context.fill(eye);
  context.restore();
}
