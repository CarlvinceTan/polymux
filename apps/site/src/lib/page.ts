import {hydrate, type Component} from 'svelte';
import '../analytics';
import '../styles/tokens.css';
import '../styles/base.css';
import '../styles/layout.css';
import '../styles/prose.css';

export function startPage<Props extends Record<string, unknown>>(component: Component<Props>) {
  const target = document.getElementById('app');
  const data = document.getElementById('page-data');
  if (!target || !data?.textContent) throw new Error('Missing prerendered page data.');
  const props = JSON.parse(data.textContent) as Props;
  hydrate(component, {target, props});
}
