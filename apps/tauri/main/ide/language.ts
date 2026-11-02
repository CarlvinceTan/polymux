import {LanguageDescription} from "@codemirror/language";
import {languages as editorLanguages} from "@codemirror/language-data";
import binaryExtensions from "binary-extensions";
import * as linguist from "linguist-languages";
import type {Language} from "linguist-languages";

export const BINARY_LANGUAGE = "Binary";
const binaryNames = new Set(binaryExtensions);
const byFilename = new Map<string, Language[]>();
const byFoldedFilename = new Map<string, Language[]>();
const byExtension = new Map<string, Language[]>();
const byInterpreter = new Map<string, Language[]>();

function index(map: Map<string, Language[]>, key: string, language: Language): void {
  const values = map.get(key) ?? [];
  if (!values.includes(language)) values.push(language);
  map.set(key, values);
}

const definitions: readonly Language[] = Object.values(linguist);
for (const language of definitions) {
  for (const name of language.filenames ?? []) {
    index(byFilename, name, language);
    index(byFoldedFilename, name.toLowerCase(), language);
  }
  for (const extension of language.extensions ?? []) index(byExtension, extension, language);
  for (const interpreter of language.interpreters ?? []) index(byInterpreter, interpreter, language);
}

/** Presentation names, independent of extension and grammar registration. */
export function languageLabel(name: string): string {
  return ({
    Shell: "Shell Script",
    JSX: "JavaScript",
    TSX: "TypeScript",
    "Ignore List": "Gitignore",
    "Go Module": "Go",
    "Go Checksums": "Go",
    LESS: "Less",
  } as Record<string, string>)[name] ?? name;
}

export function fileBaseName(name: string): string {
  return name.split(/[/\\]/).pop() ?? name;
}

/** Dotenv files include .env, .env.local, and secrets.env. */
export function isEnvFileName(name: string): boolean {
  const base = fileBaseName(name).toLowerCase();
  return base === ".env" || base.startsWith(".env.") || base.endsWith(".env");
}

/** CodeMirror's filename rules choose among ambiguous extensions such as .h. */
function chooseLanguage(candidates: Language[], base: string): Language | undefined {
  if (candidates.length === 1) return candidates[0];
  const primary = candidates.filter(language => base.endsWith(language.extensions?.[0] ?? '\0'));
  if (primary.length === 1) return primary[0];
  const editor = LanguageDescription.matchFilename(editorLanguages, base);
  if (editor) {
    return candidates.find(language => languageLabel(language.name) === languageLabel(editor.name)
      || language.aliases?.some(alias => editor.alias.includes(alias.toLowerCase())));
  }
  // Unresolved ambiguity stays plain text instead of choosing by catalog order.
  return undefined;
}

export function languageMetadataForName(name: string): Language | undefined {
  const base = fileBaseName(name);
  const lower = base.toLowerCase();
  if (isEnvFileName(base)) return linguist.Shell;
  const exact = byFilename.get(base) ?? byFoldedFilename.get(lower);
  if (exact) return chooseLanguage(exact, base);
  // Match compound suffixes first: .blade.php and .sh.in have their own entries.
  for (let dot = base.indexOf('.'); dot >= 0; dot = base.indexOf('.', dot + 1)) {
    const suffix = base.slice(dot);
    const candidates = byExtension.get(suffix) ?? byExtension.get(suffix.toLowerCase());
    if (candidates) return chooseLanguage(candidates, lower);
  }
  return undefined;
}

function interpreterLanguage(content: string): Language | undefined {
  const firstLine = content.slice(0, 1024).split(/\r?\n/, 1)[0];
  if (!firstLine.startsWith('#!')) return undefined;
  const words = firstLine.slice(2).trim().split(/\s+/);
  let command = fileBaseName(words[0] ?? '');
  if (command === 'env') {
    command = words.slice(1).find(word => !word.startsWith('-') && !word.includes('=')) ?? '';
  }
  const candidates = byInterpreter.get(command)
    ?? byInterpreter.get(command.replace(/\d+(?:\.\d+)*$/, ''));
  if (!candidates) return undefined;
  if (candidates.length === 1) return candidates[0];
  const editor = LanguageDescription.matchLanguageName(editorLanguages, command, false);
  return editor ? candidates.find(language => languageLabel(language.name) === languageLabel(editor.name)) : undefined;
}

export function isBinaryFileName(name: string): boolean {
  const base = fileBaseName(name);
  const extension = base.slice(base.lastIndexOf('.') + 1).toLowerCase();
  // A known text format takes priority when binary and source suffixes overlap.
  return binaryNames.has(extension) && !languageMetadataForName(base);
}

/** Language detection shared by the real reader, new drafts, and browser fixtures. */
export function languageForName(name: string, content = ""): string {
  const metadata = languageMetadataForName(name);
  if (metadata) return languageLabel(metadata.name);
  if (isBinaryFileName(name)) return BINARY_LANGUAGE;
  const base = fileBaseName(name);
  const editor = LanguageDescription.matchFilename(editorLanguages, base)
    ?? LanguageDescription.matchFilename(editorLanguages, base.toLowerCase());
  if (editor) return languageLabel(editor.name);
  const script = interpreterLanguage(content);
  return script ? languageLabel(script.name) : "Text";
}
