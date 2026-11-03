import {parse as parseYaml} from 'yaml';

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
