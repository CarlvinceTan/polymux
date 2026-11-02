import type {SimpleIcon} from 'simple-icons';
import {
  siAlibabacloud,
  siAnthropic,
  siCloudflare,
  siDeepseek,
  siGithubcopilot,
  siGoogle,
  siHuggingface,
  siMeta,
  siMistralai,
  siMinimax,
  siMoonshotai,
  siNvidia,
  siOpencode,
  siOpenrouter,
  siQwen,
  siVercel,
  siXiaomi,
} from 'simple-icons';

type ProviderMark = Pick<SimpleIcon, 'title' | 'slug' | 'hex' | 'path'>;

const openAi: ProviderMark = {
  title: 'OpenAI', slug: 'openai', hex: '000000',
  path: 'M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z',
};

const marks: Record<string, ProviderMark> = {
  openai: openAi,
  anthropic: siAnthropic,
  google: siGoogle,
  mistral: siMistralai,
  deepseek: siDeepseek,
  meta: siMeta,
  nvidia: siNvidia,
  'github-copilot': siGithubcopilot,
  huggingface: siHuggingface,
  cloudflare: siCloudflare,
  vercel: siVercel,
  openrouter: siOpenrouter,
  alibaba: siAlibabacloud,
  moonshotai: siMoonshotai,
  qwen: siQwen,
  minimax: siMinimax,
  xiaomi: siXiaomi,
  opencode: siOpencode,
};

const names: Record<string, string> = {
  openai: 'OpenAI', anthropic: 'Anthropic', google: 'Google', mistral: 'Mistral AI', deepseek: 'DeepSeek',
  meta: 'Meta', nvidia: 'NVIDIA', 'amazon-bedrock': 'Amazon Bedrock', 'github-copilot': 'GitHub Copilot',
  huggingface: 'Hugging Face', cloudflare: 'Cloudflare', vercel: 'Vercel', openrouter: 'OpenRouter',
  alibaba: 'Alibaba', moonshotai: 'Moonshot AI', qwen: 'Qwen', xiaomi: 'Xiaomi', opencode: 'OpenCode',
  xai: 'xAI', groq: 'Groq', cerebras: 'Cerebras', fireworks: 'Fireworks AI', together: 'Together AI',
  minimax: 'MiniMax', zai: 'Z.ai', radius: 'Radius', 'ant-ling': 'Ant Ling',
  amazon: 'Amazon', cohere: 'Cohere', ibm: 'IBM', ai21: 'AI21 Labs', azure: 'Azure AI', other: 'Other model makers',
  perplexity: 'Perplexity', microsoft: 'Microsoft', nousresearch: 'Nous Research', yi: '01.AI',
  baidu: 'Baidu', tencent: 'Tencent', stepfun: 'StepFun', upstage: 'Upstage', inflection: 'Inflection',
  liquid: 'Liquid AI', bytedance: 'ByteDance', skywork: 'Skywork', tii: 'TII', internlm: 'InternLM',
  rwkv: 'RWKV', dolphin: 'Cognitive Computations', openbmb: 'OpenBMB',
  ollama: 'Ollama', lmstudio: 'LM Studio', vllm: 'vLLM', 'llama-cpp': 'Llama.cpp',
};

const authorAliases: Record<string, string> = {
  'meta-llama': 'meta', 'mistralai': 'mistral', 'deepseek-ai': 'deepseek',
  'minimaxai': 'minimax', 'minimax-ai': 'minimax', 'qwen': 'alibaba',
  'alibaba': 'alibaba', 'hunyuan': 'tencent', 'ibm-granite': 'ibm', 'z-ai': 'zai', 'zhipuai': 'zai',
  'moonshot': 'moonshotai', 'x-ai': 'xai', 'amazon': 'amazon',
  'thudm': 'zai', '01-ai': 'yi', '01ai': 'yi', 'zero-one-ai': 'yi',
  'nous': 'nousresearch', 'nousresearch': 'nousresearch',
  'cognitivecomputations': 'dolphin', 'tiiuae': 'tii', 'liquidai': 'liquid',
  'perplexity-ai': 'perplexity', 'microsoft': 'microsoft', 'baidu': 'baidu',
  'tencent': 'tencent', 'bytedance': 'bytedance', 'bytedance-research': 'bytedance',
  'stepfun-ai': 'stepfun', 'internlm': 'internlm', 'openbmb': 'openbmb',
};

const modelGateways = new Set([
  'amazon-bedrock', 'cerebras', 'cloudpolymux-gateway', 'cloudflare-workers-ai',
  'fireworks', 'github-copilot', 'groq', 'huggingface', 'nvidia', 'opencode',
  'opencode-go', 'openrouter', 'qwen-token-plan', 'qwen-token-plan-cn', 'together',
  'vercel-ai-gateway',
]);

export function companyId(provider: string): string {
  const id = provider.toLocaleLowerCase().replace(/^~+/, '');
  if (id === 'google-vertex') return 'google';
  if (id === 'openai-codex') return 'openai';
  if (id === 'azure-openai-responses') return 'azure';
  if (id.startsWith('cloudflare-')) return 'cloudflare';
  if (id.startsWith('xiaomi')) return 'xiaomi';
  if (id.startsWith('qwen-')) return 'qwen';
  if (id.startsWith('moonshotai')) return 'moonshotai';
  if (id.startsWith('minimax')) return 'minimax';
  if (id.startsWith('zai')) return 'zai';
  if (id.startsWith('opencode')) return 'opencode';
  if (id === 'vercel-ai-gateway') return 'vercel';
  if (id === 'kimi-coding') return 'moonshotai';
  if (id.startsWith('openrouter')) return 'openrouter';
  if (id.startsWith('radius')) return 'radius';
  // Local runtimes, whose ids come from the preset's name and may carry a
  // suffix when the user already has one of that runtime configured.
  if (id.startsWith('lm-studio') || id.startsWith('lmstudio')) return 'lmstudio';
  if (id.startsWith('ollama')) return 'ollama';
  if (id.startsWith('vllm')) return 'vllm';
  if (id.startsWith('llama-cpp') || id.startsWith('llama.cpp')) return 'llama-cpp';
  return id;
}

export function providerName(provider: string): string {
  const id = companyId(provider);
  return names[id] ?? id.split('-').map((word) => word[0]?.toUpperCase() + word.slice(1)).join(' ');
}

export function providerMark(provider: string): ProviderMark | undefined {
  return marks[companyId(provider)];
}

/** Identifies the lab that made a model rather than the service routing it.
 * OpenRouter's canonical `author/model` convention is preferred whenever the
 * catalogue includes it, followed by well-known model-family identifiers. */
export function modelCompanyId(model: {provider: string; id: string; name: string}): string {
  const provider = model.provider.toLocaleLowerCase();
  const id = model.id.toLocaleLowerCase().replace(/^@cf\//, '');
  const namespaced = namespaceAuthor(id);
  if (namespaced) return namespaced;
  if (id.startsWith('anthropic.')) return 'anthropic';
  if (id.startsWith('amazon.')) return 'amazon';

  const value = `${id} ${model.name.toLocaleLowerCase()}`;
  const families: Array<[RegExp, string]> = [
    [/\bclaude\b|\banthropic\b/, 'anthropic'],
    [/\bgpt[- .]|\bo[134][-.]|\bopenai\b/, 'openai'],
    [/\bgemini\b|\bgemma\b/, 'google'],
    [/\bllama\b/, 'meta'],
    [/\bmistral\b|\bcodestral\b|\bdevstral\b|\bministral\b/, 'mistral'],
    [/\bdeepseek\b/, 'deepseek'],
    // Version suffixes run straight into the family name (`qwen3.7-max`,
    // `hy3`), so these can't end on a word boundary.
    [/\bqwen|\bqwq\b|\bqvq\b/, 'alibaba'],
    [/\bhunyuan\b|\bhy\d/, 'tencent'],
    [/\bkimi\b|\bmoonshot\b/, 'moonshotai'],
    [/\bminimax\b/, 'minimax'],
    [/\bglm\b|\bz\.ai\b/, 'zai'],
    [/\bmimo\b/, 'xiaomi'],
    [/\bgrok\b/, 'xai'],
    [/\bnova\b/, 'amazon'],
    [/\bcommand[- ]|\bcohere\b/, 'cohere'],
    [/\bjamba\b/, 'ai21'],
    [/\bgranite\b/, 'ibm'],
    [/\bnemotron\b/, 'nvidia'],
    [/\bling[- .]|\bring[- .]/, 'ant-ling'],
  ];
  for (const [pattern, author] of families) if (pattern.test(value)) return author;
  return modelGateways.has(provider) ? 'other' : companyId(provider);
}

function namespaceAuthor(id: string): string | undefined {
  const parts = id.split('/');
  if (parts.length < 2 || parts[0] === 'accounts') return undefined;
  const raw = (parts[0] === 'cf' ? parts[1] : parts[0])?.replace(/^~+/, '');
  if (!raw) return undefined;
  // Any `author/model` namespace names a real lab, so trust it rather than
  // matching against a fixed roster — an unrecognised author is still that
  // author, and collapsing it into "Other" loses the one reliable signal the
  // catalogue gives us. Unknown ids fall back to a title-cased name and
  // initials in place of a logo.
  return authorAliases[raw] ?? raw;
}
