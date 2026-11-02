/**
 * The edge fade the settings rails carry, as one behaviour every scrollable
 * column can share: solid where the content ends, faded where it runs on, and
 * the fade at an edge gone the moment the list is scrolled hard against it.
 *
 * The element supplies the mask through the `scroll-fade` class in the global
 * stylesheet; this only keeps `at-top`/`at-bottom` honest. Measurements are
 * re-taken on scroll, on a resize of the column, and on any change to what is
 * inside it, so a filtered list or a panel that swapped its contents never
 * leaves a fade over an edge that no longer scrolls.
 */
export function scrollFade(node: HTMLElement, _cue?: unknown) {
  node.classList.add('scroll-fade');

  function measure(): void {
    // A column that does not scroll is solid at both ends rather than faded at
    // one: sub-pixel layout can leave a stray fraction of overflow, so the
    // comparison is deliberately slack.
    node.classList.toggle('at-top', node.scrollTop <= 1);
    node.classList.toggle(
      'at-bottom',
      node.scrollHeight - node.scrollTop - node.clientHeight <= 1,
    );
  }

  measure();
  node.addEventListener('scroll', measure, {passive: true});
  const resize = new ResizeObserver(measure);
  resize.observe(node);
  // Rows are added and removed without the column's own box changing, which a
  // ResizeObserver on the column alone would not see.
  const mutations = new MutationObserver(measure);
  mutations.observe(node, {childList: true, subtree: true, characterData: true});

  return {
    // Called when the value passed to the action changes — a filter, a tab, or
    // any other cue the caller wants a fresh measurement on.
    update: measure,
    destroy(): void {
      node.removeEventListener('scroll', measure);
      resize.disconnect();
      mutations.disconnect();
    },
  };
}

/**
 * The same behaviour turned on its side, for a row that scrolls horizontally:
 * faded at an edge the content runs past, solid at an edge the row is scrolled
 * hard against, so the leftmost tab at the far left — and the rightmost at the
 * far right — is never dimmed by a fade that means "there is more this way".
 */
export function scrollFadeX(node: HTMLElement, _cue?: unknown) {
  node.classList.add('scroll-fade-x');

  function measure(): void {
    node.classList.toggle('at-start', node.scrollLeft <= 1);
    node.classList.toggle(
      'at-end',
      node.scrollWidth - node.scrollLeft - node.clientWidth <= 1,
    );
  }

  measure();
  node.addEventListener('scroll', measure, {passive: true});
  const resize = new ResizeObserver(measure);
  resize.observe(node);
  const mutations = new MutationObserver(measure);
  mutations.observe(node, {childList: true, subtree: true, characterData: true});

  return {
    update: measure,
    destroy(): void {
      node.removeEventListener('scroll', measure);
      resize.disconnect();
      mutations.disconnect();
    },
  };
}
