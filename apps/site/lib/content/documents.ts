import {parse as parseYaml} from 'yaml';
import type {ProductFeature, ProductFeatureIcon} from './types.ts';

export function readDocument(source: string, filename: string) {
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)([\s\S]*)$/);
  if (!match) throw new Error(`${filename} is missing YAML front matter.`);
  const metadata: unknown = parseYaml(match[1]);
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) {
    throw new Error(`${filename} has invalid YAML front matter.`);
  }
  const values = metadata as Record<string, unknown>;
  if (values.published !== undefined && typeof values.published !== 'boolean') {
    throw new Error(`${filename}: published must be a boolean.`);
  }
  return {metadata: values, body: match[2].trim(), published: values.published !== false};
}

export function text(value: unknown, field: string, filename: string): string {
  if (typeof value !== 'string' || !value.trim()) throw new Error(`${filename} requires ${field}.`);
  return value.trim();
}

export function slug(value: unknown, filename: string): string {
  const result = text(value, 'a slug', filename);
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(result)) throw new Error(`${filename} has an invalid slug: ${result}`);
  return result;
}

export function date(value: unknown, field: string, filename: string): string {
  const result = text(value, field, filename);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(result) || Number.isNaN(Date.parse(`${result}T00:00:00Z`)) ||
      new Date(`${result}T00:00:00Z`).toISOString().slice(0, 10) !== result) {
    throw new Error(`${filename} has an invalid ${field}: ${result}`);
  }
  return result;
}

export function updated(value: unknown, filename: string): string | undefined {
  return value === undefined || value === null || value === '' ? undefined : date(value, 'updated date', filename);
}

export function unique(values: string[], field: string) {
  const seen = new Set<string>();
  for (const value of values) {
    if (seen.has(value)) throw new Error(`Duplicate ${field}: ${value}`);
    seen.add(value);
  }
}

export function integer(value: unknown, field: string, filename: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 1) {
    throw new Error(`${filename} requires a positive integer ${field}.`);
  }
  return value;
}

function record(value: unknown, field: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`Product ${field} must be an object.`);
  return value as Record<string, unknown>;
}

function list(value: unknown, field: string): unknown[] {
  if (!Array.isArray(value) || !value.length) throw new Error(`Product ${field} must be a nonempty array.`);
  return value;
}

export function readFeatures(value: unknown): ProductFeature[] {
  const icons = new Set<ProductFeatureIcon>(['workspace', 'hub', 'drive', 'browser', 'memory', 'tasks', 'models', 'skills']);
  const features = list(value, 'features').map((item) => {
    const feature = record(item, 'feature');
    const name = slug(feature.slug, 'Product feature');
    const string = (key: string) => text(feature[key], key, name);
    if (feature.group !== 'app' && feature.group !== 'agent') throw new Error(`${name} has an invalid group.`);
    if (!icons.has(feature.icon as ProductFeatureIcon)) throw new Error(`${name} has an invalid icon.`);
    const details = (key: string) => list(feature[key], key).map((item) => {
      const entry = record(item, key);
      return {title: text(entry.title, 'title', name), description: text(entry.description, 'description', name)};
    });
    const demoItems = list(feature.demoItems, 'demoItems').map((item) => {
      const entry = record(item, 'demo item');
      return {label: text(entry.label, 'label', name), title: text(entry.title, 'title', name), description: text(entry.description, 'description', name)};
    });
    const example = record(feature.example, 'example');
    const docsPath = string('docsPath');
    if (!/^\/docs\/(?:[a-z0-9]+(?:-[a-z0-9]+)*\/)?(?:#[a-z0-9-]+)?$/.test(docsPath)) throw new Error(`${name} has an invalid docsPath.`);
    return {
      group: feature.group, slug: name, icon: feature.icon as ProductFeatureIcon,
      name: string('name'), menuDescription: string('menuDescription'), title: string('title'),
      description: string('description'), intro: string('intro'), demoTitle: string('demoTitle'),
      demoItems, benefits: details('benefits'), steps: list(feature.steps, 'steps').map((step) => text(step, 'step', name)),
      example: {title: text(example.title, 'title', name), description: text(example.description, 'description', name)},
      docsPath, updated: updated(feature.updated, name),
    } satisfies ProductFeature;
  });
  unique(features.map((feature) => feature.slug), 'product slug');
  return features;
}
