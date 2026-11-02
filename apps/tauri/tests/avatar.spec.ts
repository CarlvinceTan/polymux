import { expect, test } from '@playwright/test';
import reference from './fixtures/avatar-shape-reference.json' with { type: 'json' };
import characters from './fixtures/avatar-character-reference.json' with { type: 'json' };
import catReference from './fixtures/avatar-cat-reference.json' with { type: 'json' };
import dogReference from './fixtures/avatar-dog-reference.json' with { type: 'json' };
import bearReference from './fixtures/avatar-bear-reference.json' with { type: 'json' };
import rabbitReference from './fixtures/avatar-rabbit-reference.json' with { type: 'json' };
import foxReference from './fixtures/avatar-fox-reference.json' with { type: 'json' };
import pigReference from './fixtures/avatar-pig-reference.json' with { type: 'json' };
import deerReference from './fixtures/avatar-deer-reference.json' with { type: 'json' };
import cowReference from './fixtures/avatar-cow-reference.json' with { type: 'json' };
import tigerReference from './fixtures/avatar-tiger-reference.json' with { type: 'json' };
import hamsterReference from './fixtures/avatar-hamster-reference.json' with { type: 'json' };
import capybaraReference from './fixtures/avatar-capybara-reference.json' with { type: 'json' };
import otterReference from './fixtures/avatar-otter-reference.json' with { type: 'json' };
import sheepReference from './fixtures/avatar-sheep-reference.json' with { type: 'json' };
import alpacaReference from './fixtures/avatar-alpaca-reference.json' with { type: 'json' };
import squirrelReference from './fixtures/avatar-squirrel-reference.json' with { type: 'json' };
import lionReference from './fixtures/avatar-lion-reference.json' with { type: 'json' };
import hedgehogReference from './fixtures/avatar-hedgehog-reference.json' with { type: 'json' };
import sealReference from './fixtures/avatar-seal-reference.json' with { type: 'json' };
import monkeyReference from './fixtures/avatar-monkey-reference.json' with { type: 'json' };
import penguinReference from './fixtures/avatar-penguin-reference.json' with { type: 'json' };
import owlReference from './fixtures/avatar-owl-reference.json' with { type: 'json' };
import parrotReference from './fixtures/avatar-parrot-reference.json' with { type: 'json' };
import whaleReference from './fixtures/avatar-whale-reference.json' with { type: 'json' };
import bullReference from './fixtures/avatar-bull-reference.json' with { type: 'json' };
import beaverReference from './fixtures/avatar-beaver-reference.json' with { type: 'json' };
import guineaPigReference from './fixtures/avatar-guinea_pig-reference.json' with { type: 'json' };
import chinchillaReference from './fixtures/avatar-chinchilla-reference.json' with { type: 'json' };
import ferretReference from './fixtures/avatar-ferret-reference.json' with { type: 'json' };
import chickReference from './fixtures/avatar-chick-reference.json' with { type: 'json' };
import duckReference from './fixtures/avatar-duck-reference.json' with { type: 'json' };
import gooseReference from './fixtures/avatar-goose-reference.json' with { type: 'json' };
import extendedReference from './fixtures/avatar-extended-reference.json' with { type: 'json' };

// Samples come from the reference still frame, in the same 100-unit coordinate space.
test('abstract shape silhouettes match the reference', async ({ page }) => {
  await page.goto('/');
  const mismatches = await page.evaluate(async (samples) => {
    const { shapePaths } = await import('/lib/avatar/kit/solid.ts');
    const context = document.createElement('canvas').getContext('2d')!;
    return Object.entries(samples).flatMap(([id, { checks, eyeChecks }]) => {
      const paths = shapePaths(id);
      if (!paths) return [`${id}: missing shape`];
      const errors = [];
      for (let y = 0; y < 21; y++) for (let x = 0; x < 21; x++) {
        if (context.isPointInPath(paths.body, -119.3 + x * 11.9, -119.1 + y * 11.9) !== (checks[y * 21 + x] === '1')) {
          errors.push(`${x},${y}`);
        }
      }
      for (let y = 0; y < 31; y++) for (let x = 0; x < 31; x++) {
        if (context.isPointInPath(paths.eyes, -70.3 + x * 4.7, -70.1 + y * 4.7) !== (eyeChecks[y * 31 + x] === '1')) {
          errors.push(`eye ${x},${y}`);
        }
      }
      return errors.length ? [`${id}: ${errors.join(' ')}`] : [];
    });
  }, reference);
  expect(mismatches).toEqual([]);
});

test('plus silhouette and eyes match the reference', async ({ page }) => {
  await page.goto('/');
  const actual = await page.evaluate(async () => {
    const { shapePaths } = await import('/lib/avatar/kit/solid.ts');
    const shape = shapePaths('plus')!;
    const context = document.createElement('canvas').getContext('2d')!;
    const sample = (path: Path2D) => Array.from({ length: 25 }, (_, y) => Array.from({ length: 25 }, (_, x) =>
      context.isPointInPath(path, -139.3 + x * 11.6, -139.1 + y * 11.6) ? '1' : '0').join('')).join('');
    return { body: sample(shape.body), eyes: sample(shape.eyes) };
  });
  expect(actual.body).toBe(characters.plus.body);
  // The two separate reference eye paths share one rendered fill.
  expect(actual.eyes).toBe(characters.plus.eyes[0].split('').map((point, index) =>
    point === '1' || characters.plus.eyes[1][index] === '1' ? '1' : '0').join(''));
});

for (const [id, reference] of [
  ['cat', catReference.cat], ['dog', dogReference.dog], ['bear', bearReference.bear], ['rabbit', rabbitReference.rabbit],
  ['fox', foxReference.fox], ['pig', pigReference.pig], ['deer', deerReference.deer], ['cow', cowReference.cow],
  ['tiger', tigerReference.tiger], ['hamster', hamsterReference.hamster], ['capybara', capybaraReference.capybara],
  ['otter', otterReference.otter], ['sheep', sheepReference.sheep], ['alpaca', alpacaReference.alpaca],
  ['squirrel', squirrelReference.squirrel], ['lion', lionReference.lion], ['hedgehog', hedgehogReference.hedgehog],
  ['seal', sealReference.seal], ['monkey', monkeyReference.monkey], ['penguin', penguinReference.penguin],
  ['owl', owlReference.owl], ['parrot', parrotReference.parrot], ['whale', whaleReference.whale], ['bull', bullReference.bull],
  ['beaver', beaverReference.beaver], ['guinea_pig', guineaPigReference.guinea_pig],
  ['chinchilla', chinchillaReference.chinchilla], ['ferret', ferretReference.ferret],
  ['chick', chickReference.chick], ['duck', duckReference.duck], ['goose', gooseReference.goose],
  ...Object.entries(extendedReference),
] as const) test(`${id} geometry and layered colors match the reference resting frame`, async ({ page }) => {
  await page.goto('/avatar-test.html');
  const actual = await page.evaluate(async ({ id, pixels }) => {
    const { loadCharacter, drawCharacter } = await import('/lib/avatar/kit/character.ts');
    const character = (await loadCharacter(id))!;
    const canvas = document.createElement('canvas');
    canvas.width = 316; canvas.height = 316;
    const context = canvas.getContext('2d')!;
    const sample = (path: Path2D) => Array.from({ length: 25 }, (_, y) => Array.from({ length: 25 }, (_, x) =>
      context.isPointInPath(path, -139.3 + x * 11.6, -139.1 + y * 11.6) ? '1' : '0').join('')).join('');
    const paths = {
      body: sample(character.body),
      parts: character.parts.map((part) => sample(part.path)),
      eyes: character.eyes.map(sample),
    };
    context.translate(158, 158);
    drawCharacter(context, character);
    const colors = (pixels as [number, number, string][]).map(([x, y]) => {
      const rgba = context.getImageData(Math.floor(x + 158), Math.floor(y + 158), 1, 1).data;
      return rgba[3] === 0 ? 'transparent' : `#${[...rgba.slice(0, 3)].map((value) => value.toString(16).padStart(2, '0')).join('').toUpperCase()}`;
    });
    return { ...paths, colors };
  }, { id, pixels: reference.pixels });
  expect(actual.body).toBe(reference.body);
  expect(actual.parts).toEqual(reference.parts);
  expect(actual.eyes).toEqual(reference.eyes);
  const mismatches = actual.colors.flatMap((color, index) => {
    const expected = reference.pixels[index][2] as string;
    if (color === expected) return [];
    if (color === 'transparent' || expected === 'transparent') return [`${index}: ${color} vs ${expected}`];
    // Small channel differences come from antialiasing when a mesh edge crosses a sampled pixel.
    const close = [1, 3, 5].every((i) => Math.abs(parseInt(color.slice(i, i + 2), 16) - parseInt(expected.slice(i, i + 2), 16)) <= 6);
    return close ? [] : [`${index}: ${color} vs ${expected}`];
  });
  expect(mismatches).toEqual([]);
});

test('open avatars redraw when appearance changes', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const avatar = page.locator('.chat-header canvas').first();
  await expect(avatar).toBeVisible();
  const pixel = () => avatar.evaluate((canvas: HTMLCanvasElement) => {
    const data = canvas.getContext('2d')!.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height * 0.6), 1, 1).data;
    return [...data];
  });
  await expect.poll(pixel).toEqual([0, 0, 0, 255]);
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('combobox', { name: 'Theme' }).selectOption('dark');
  await expect.poll(pixel).toEqual([255, 255, 255, 255]);
  await page.getByRole('combobox', { name: 'Theme' }).selectOption('light');
  await expect.poll(pixel).toEqual([0, 0, 0, 255]);
  await page.getByRole('combobox', { name: 'Theme' }).selectOption('system');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(pixel).toEqual([255, 255, 255, 255]);
  expect(await page.evaluate(() => localStorage.getItem('polymux.appearance'))).toBe('system');
  await page.getByRole('combobox', { name: 'Theme' }).selectOption('light');
  await page.emulateMedia({ colorScheme: 'light' });
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(pixel).toEqual([0, 0, 0, 255]);
  expect(await page.evaluate(() => localStorage.getItem('polymux.appearance'))).toBe('light');
  await page.getByRole('combobox', { name: 'Theme' }).selectOption('system');
  await expect.poll(pixel).toEqual([255, 255, 255, 255]);
  await page.emulateMedia({ colorScheme: 'light' });
  await expect.poll(pixel).toEqual([0, 0, 0, 255]);
});

for (const mode of ['light', 'dark'] as const) test(`choosing a shape redraws the bot preview (${mode})`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: mode });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'New Bot', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'New Bot' });
  const canvas = dialog.locator('.preview canvas');
  const area = () => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    let count = 0;
    for (let i = 3; i < pixels.length; i += 4) if (pixels[i]) count++;
    return count;
  });
  const square = dialog.getByRole('button', { name: 'Square', exact: true });
  await square.click();
  await expect(square).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(area).toBeGreaterThan(0);
  const squareArea = await area();
  const hexagon = dialog.getByRole('button', { name: 'Hexagon', exact: true });
  await hexagon.click();
  await expect(hexagon).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(area).not.toBe(squareArea);
  const hexagonArea = await area();
  const plus = dialog.getByRole('button', { name: 'Plus', exact: true });
  await plus.click();
  await expect(plus).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(area).not.toBe(hexagonArea);
  await dialog.getByRole('button', { name: 'Animals', exact: true }).click();
  const cat = dialog.getByRole('button', { name: 'Cat', exact: true });
  await cat.click();
  await expect(cat).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 135 && pixels[i + 1] === 150 && pixels[i + 2] === 172) return true;
    return false;
  })).toBe(true);
  const dog = dialog.getByRole('button', { name: 'Dog', exact: true });
  await dog.click();
  await expect(dog).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 195 && pixels[i + 1] === 148 && pixels[i + 2] === 96) return true;
    return false;
  })).toBe(true);
  const panda = dialog.getByRole('button', { name: 'Panda', exact: true });
  await panda.click();
  await expect(panda).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 238 && pixels[i + 1] === 237 && pixels[i + 2] === 230) return true;
    return false;
  })).toBe(true);
  const rabbit = dialog.getByRole('button', { name: 'Rabbit', exact: true });
  await rabbit.click();
  await expect(rabbit).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 172 && pixels[i + 1] === 133 && pixels[i + 2] === 156) return true;
    return false;
  })).toBe(true);
  const fox = dialog.getByRole('button', { name: 'Fox', exact: true });
  await fox.click();
  await expect(fox).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 224 && pixels[i + 1] === 119 && pixels[i + 2] === 58) return true;
    return false;
  })).toBe(true);
  const pig = dialog.getByRole('button', { name: 'Pig', exact: true });
  await pig.click();
  await expect(pig).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => {
    const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === 214 && pixels[i + 1] === 142 && pixels[i + 2] === 140) return true;
    return false;
  })).toBe(true);
  for (const [name, color] of [
    ['Deer', [183, 122, 76]], ['Cow', [180, 165, 142]], ['Tiger', [223, 150, 80]],
    ['Crab', [213, 107, 86]], ['Whale', [77, 107, 254]],
  ] as const) {
    const choice = dialog.getByRole('button', { name, exact: true });
    await choice.click();
    await expect(choice).toHaveAttribute('aria-pressed', 'true');
    await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement, color) => {
      const pixels = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
      for (let i = 0; i < pixels.length; i += 4) if (pixels[i] === color[0] && pixels[i + 1] === color[1] && pixels[i + 2] === color[2]) return true;
      return false;
    }, color)).toBe(true);
  }
});
