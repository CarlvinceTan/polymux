import atlas from '../assets/agents/atlas.webp';
import nova from '../assets/agents/nova.webp';
import fern from '../assets/agents/fern.webp';
import strawberry from '../assets/agents/strawberry.webp';
import whale from '../assets/agents/whale.webp';
import penguin from '../assets/agents/penguin.webp';
import fox from '../assets/agents/fox.webp';
import heart from '../assets/agents/heart.webp';
import cat from '../assets/agents/cat.webp';
import dog from '../assets/agents/dog.webp';
import rabbit from '../assets/agents/rabbit.webp';
import bear from '../assets/agents/bear.webp';

// Animated captures of the app's own avatars, keyed onto transparency.
export const agents = [
  {id: 'atlas', src: atlas, tint: '#1786ff'},
  {id: 'nova', src: nova, tint: '#9157ff'},
  {id: 'fern', src: fern, tint: '#00c46e'},
  {id: 'strawberry', src: strawberry, tint: '#e0344f'},
  {id: 'whale', src: whale, tint: '#4f7dff'},
  {id: 'penguin', src: penguin, tint: '#f2a33a'},
  {id: 'fox', src: fox, tint: '#ef7b33'},
  {id: 'heart', src: heart, tint: '#ff3aa0'},
  {id: 'cat', src: cat, tint: '#22d3c5'},
  {id: 'dog', src: dog, tint: '#f5b52e'},
  {id: 'rabbit', src: rabbit, tint: '#c084fc'},
  {id: 'bear', src: bear, tint: '#d99a6c'},
] as const;

export type Agent = typeof agents[number];
export const agent = (id: Agent['id']) => agents.find((item) => item.id === id)!;
