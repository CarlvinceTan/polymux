// Card kit runtime: segmented controls, slider fill, theme colours, and Chart.js theming.
(() => {
  const root = document.documentElement;

  // Segmented controls: one pressed button; change carries detail.value.
  const select = (group, button, notify) => {
    for (const option of group.querySelectorAll(':scope > button')) {
      option.setAttribute('aria-pressed', option === button ? 'true' : 'false');
    }
    const value = button.value || button.textContent.trim();
    if (group.dataset.value !== value) group.dataset.value = value;
    group.value = value;
    if (notify) group.dispatchEvent(new CustomEvent('change', { bubbles: true, detail: { value } }));
  };
  const setup = (group) => {
    const options = [...group.querySelectorAll(':scope > button')].filter((button) => !button.matches(':disabled'));
    if (!options.length) return;
    if (!group.hasAttribute('role')) group.setAttribute('role', 'group');
    for (const button of options) if (!button.hasAttribute('type')) button.type = 'button';
    const pressed = options.find((b) => b.value === group.dataset.value)
      || options.find((b) => b.getAttribute('aria-pressed') === 'true') || options[0];
    select(group, pressed, false);
  };
  document.addEventListener('click', (event) => {
    const button = event.target.closest?.('.segmented > button');
    if (!button || button.matches(':disabled') || button.getAttribute('aria-pressed') === 'true') return;
    select(button.parentElement, button, true);
  });
  document.addEventListener('keydown', (event) => {
    const button = event.target.closest?.('.segmented > button');
    if (!button || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const options = [...button.parentElement.querySelectorAll(':scope > button')].filter((item) => !item.matches(':disabled'));
    if (!options.length) return;
    event.preventDefault();
    const index = options.indexOf(button);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1
      : (index + (event.key === 'ArrowRight' ? 1 : -1) + options.length) % options.length;
    options[next].focus();
    if (options[next] !== button) select(button.parentElement, options[next], true);
  });

  // Range sliders paint their filled part from --fill-to.
  const paint = (input) => {
    const min = Number(input.min || 0), max = Number(input.max || 100);
    const share = max > min ? (Number(input.value) - min) / (max - min) : 0;
    input.style.setProperty('--fill-to', Math.min(100, Math.max(0, share * 100)) + '%');
  };
  const value = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
  Object.defineProperty(HTMLInputElement.prototype, 'value', {
    configurable: true,
    get() { return value.get.call(this); },
    set(next) { value.set.call(this, next); if (this.type === 'range') paint(this); },
  });
  document.addEventListener('input', (event) => {
    if (event.target instanceof HTMLInputElement && event.target.type === 'range') paint(event.target);
  }, true);

  const scan = (node) => {
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    if (node.matches('.segmented')) setup(node);
    if (node.matches('input[type=range]')) paint(node);
    node.querySelectorAll('.segmented').forEach(setup);
    node.querySelectorAll('input[type=range]').forEach(paint);
  };
  const start = () => {
    scan(document.body);
    new MutationObserver((changes) => {
      for (const change of changes) {
        if (change.type === 'attributes') scan(change.target);
        else {
          if (change.target.matches?.('.segmented')) setup(change.target);
          change.addedNodes.forEach(scan);
        }
      }
    }).observe(document.body, { childList: true, subtree: true, attributes: true,
      attributeFilter: ['min', 'max', 'value', 'disabled', 'data-value'] });
    document.addEventListener('reset', () => queueMicrotask(() => scan(document.body)));
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
  else start();

  // Chart.js: concrete colours per appearance, since canvas can't read CSS variables.
  const palette = () => {
    const style = getComputedStyle(root);
    return Array.from({ length: 8 }, (_, i) => style.getPropertyValue('--series-' + (i + 1)).trim());
  };
  const chrome = {
    light: { text: '#1d1d1f', muted: '#5f5f5f', grid: 'rgba(0,0,0,0.08)', base: 'rgba(0,0,0,0.3)', raised: '#ffffff', line: 'rgba(0,0,0,0.12)' },
    dark: { text: '#f2f2f2', muted: '#adadad', grid: 'rgba(255,255,255,0.1)', base: 'rgba(255,255,255,0.3)', raised: '#3a3a3a', line: 'rgba(255,255,255,0.14)' },
  };
  const mode = () => (root.dataset.theme === 'dark' ? 'dark' : 'light');
  const alpha = (hex, amount) => hex + Math.round(amount * 255).toString(16).padStart(2, '0');
  const radial = ['pie', 'doughnut', 'polarArea'];

  const theme = () => {
    const Chart = window.Chart;
    if (!Chart) return;
    const d = Chart.defaults;
    d.font.family = '-apple-system, BlinkMacSystemFont, system-ui, sans-serif';
    d.font.size = 12;
    d.color = () => chrome[mode()].muted;
    d.borderColor = () => chrome[mode()].grid;
    d.maintainAspectRatio = false;
    d.animation = false;
    d.interaction.mode = 'index';
    d.interaction.intersect = false;
    d.scale.grid.color = () => chrome[mode()].grid;
    d.scale.border.color = () => chrome[mode()].base;
    d.elements.line.borderWidth = 2;
    d.elements.point.radius = 0;
    d.elements.point.hoverRadius = 5;
    d.elements.point.hitRadius = 12;
    d.elements.bar.borderRadius = 4;
    d.elements.arc.borderWidth = 2;
    d.elements.arc.borderColor = () => getComputedStyle(root).getPropertyValue('--surface').trim() || chrome[mode()].raised;
    const legend = d.plugins.legend.labels;
    legend.usePointStyle = true;
    legend.boxWidth = legend.boxHeight = 8;
    legend.padding = 12;
    legend.color = () => chrome[mode()].text;
    Object.assign(d.plugins.tooltip, {
      backgroundColor: () => chrome[mode()].raised,
      titleColor: () => chrome[mode()].text,
      bodyColor: () => chrome[mode()].text,
      footerColor: () => chrome[mode()].muted,
      borderColor: () => chrome[mode()].line,
      borderWidth: 1, cornerRadius: 8, padding: 8, boxPadding: 4, usePointStyle: true,
    });
  };

  // Datasets without colours take the series palette in order, and follow the theme.
  const paletteFill = {
    id: 'polymuxPalette',
    beforeUpdate(chart) {
      const colour = (index) => {
        const colours = palette();
        return colours[index % colours.length];
      };
      const type = chart.config.type;
      chart.data.datasets.forEach((set, index) => {
        const own = set.polymuxOwn || (set.polymuxOwn = {
          border: set.borderColor === undefined, background: set.backgroundColor === undefined,
          point: set.pointBackgroundColor === undefined,
        });
        if (radial.includes(set.type || type)) {
          if (own.background) set.backgroundColor = (context) => colour(context.dataIndex || 0);
          return;
        }
        const kind = set.type || type;
        if (own.border) set.borderColor = () => colour(index);
        if (own.background) set.backgroundColor = () => kind === 'line' || kind === 'radar'
          ? alpha(colour(index), set.fill ? 0.16 : 1) : colour(index);
        if (own.point) set.pointBackgroundColor = () => colour(index);
      });
    },
  };

  if (window.Chart) {
    theme();
    window.Chart.register(paletteFill);
  }

  // Resolves a colour token to rgba() for canvas and WebGL, which can't read CSS variables.
  let probe, swatch;
  window.polymuxColor = (name) => {
    if (!probe) {
      probe = document.head.appendChild(document.createElement('i'));
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 1;
      swatch = canvas.getContext('2d', { willReadFrequently: true });
    }
    probe.style.color = 'var(' + name + ')';
    swatch.clearRect(0, 0, 1, 1);
    swatch.fillStyle = getComputedStyle(probe).color;
    swatch.fillRect(0, 0, 1, 1);
    const [r, g, b, a] = swatch.getImageData(0, 0, 1, 1).data;
    return 'rgba(' + r + ', ' + g + ', ' + b + ', ' + Math.round(a / 2.55) / 100 + ')';
  };

  // Called by the app when the appearance changes.
  window.polymuxTheme = () => {
    theme();
    if (window.Chart) Object.values(window.Chart.instances).forEach((chart) => {
      chart.update('none');
      chart.draw();
    });
    window.dispatchEvent(new Event('polymux:theme'));
  };
})();
