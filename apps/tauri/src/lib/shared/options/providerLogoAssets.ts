// Brand-colour marks where lobehub ships a square `-color` variant. The rest
// have no colour variant because the real logo is monochrome (OpenAI, Anthropic,
// xAI, Vercel, Groq, Moonshot, Z.ai, IBM, GitHub Copilot, OpenCode, Xiaomi MiMo)
// — those stay on the plain mark rather than getting a tinted stand-in.
import alibaba from '@lobehub/icons-static-svg/icons/alibaba-color.svg?url';
import antGroup from '@lobehub/icons-static-svg/icons/antgroup-color.svg?url';
import aws from '@lobehub/icons-static-svg/icons/aws-color.svg?url';
import azure from '@lobehub/icons-static-svg/icons/azureai-color.svg?url';
import bedrock from '@lobehub/icons-static-svg/icons/bedrock-color.svg?url';
import cerebras from '@lobehub/icons-static-svg/icons/cerebras-color.svg?url';
import cloudflare from '@lobehub/icons-static-svg/icons/cloudflare-color.svg?url';
import cohere from '@lobehub/icons-static-svg/icons/cohere-color.svg?url';
import deepseek from '@lobehub/icons-static-svg/icons/deepseek-color.svg?url';
import fireworks from '@lobehub/icons-static-svg/icons/fireworks-color.svg?url';
import google from '@lobehub/icons-static-svg/icons/google-color.svg?url';
import huggingface from '@lobehub/icons-static-svg/icons/huggingface-color.svg?url';
import meta from '@lobehub/icons-static-svg/icons/meta-color.svg?url';
import minimax from '@lobehub/icons-static-svg/icons/minimax-color.svg?url';
import mistral from '@lobehub/icons-static-svg/icons/mistral-color.svg?url';
import nvidia from '@lobehub/icons-static-svg/icons/nvidia-color.svg?url';
import openrouter from '@lobehub/icons-static-svg/icons/openrouter-color.svg?url';
import qwen from '@lobehub/icons-static-svg/icons/qwen-color.svg?url';
import together from '@lobehub/icons-static-svg/icons/together-color.svg?url';
import vertex from '@lobehub/icons-static-svg/icons/vertexai-color.svg?url';
import workers from '@lobehub/icons-static-svg/icons/workersai-color.svg?url';

import ai21 from '@lobehub/icons-static-svg/icons/ai21.svg?url';
import anthropic from '@lobehub/icons-static-svg/icons/anthropic.svg?url';
import githubCopilot from '@lobehub/icons-static-svg/icons/githubcopilot.svg?url';
import groq from '@lobehub/icons-static-svg/icons/groq.svg?url';
import ibm from '@lobehub/icons-static-svg/icons/ibm.svg?url';
import moonshot from '@lobehub/icons-static-svg/icons/moonshot.svg?url';
import opencode from '@lobehub/icons-static-svg/icons/opencode.svg?url';
import openai from '@lobehub/icons-static-svg/icons/openai.svg?url';
import vercel from '@lobehub/icons-static-svg/icons/vercel.svg?url';
import xai from '@lobehub/icons-static-svg/icons/xai.svg?url';
import xiaomi from '@lobehub/icons-static-svg/icons/xiaomimimo.svg?url';
import zai from '@lobehub/icons-static-svg/icons/zai.svg?url';
import llamaCpp from '../../../assets/providers/llama-cpp.svg?url';

// Labs that reach us through an `author/model` namespace rather than as a
// configured provider. Without these they'd render as initials.
import baidu from '@lobehub/icons-static-svg/icons/baidu-color.svg?url';
import bytedance from '@lobehub/icons-static-svg/icons/bytedance-color.svg?url';
import internlm from '@lobehub/icons-static-svg/icons/internlm-color.svg?url';
import microsoft from '@lobehub/icons-static-svg/icons/microsoft-color.svg?url';
import perplexity from '@lobehub/icons-static-svg/icons/perplexity-color.svg?url';
import rwkv from '@lobehub/icons-static-svg/icons/rwkv-color.svg?url';
import skywork from '@lobehub/icons-static-svg/icons/skywork-color.svg?url';
import stepfun from '@lobehub/icons-static-svg/icons/stepfun-color.svg?url';
import tencent from '@lobehub/icons-static-svg/icons/tencent-color.svg?url';
import tii from '@lobehub/icons-static-svg/icons/tii-color.svg?url';
import upstage from '@lobehub/icons-static-svg/icons/upstage-color.svg?url';
import yi from '@lobehub/icons-static-svg/icons/yi-color.svg?url';
import dolphin from '@lobehub/icons-static-svg/icons/dolphin.svg?url';
import inflection from '@lobehub/icons-static-svg/icons/inflection.svg?url';
import liquid from '@lobehub/icons-static-svg/icons/liquid.svg?url';
import nousresearch from '@lobehub/icons-static-svg/icons/nousresearch.svg?url';
import {companyId} from './providerBrands';

const logos: Record<string, string> = {
  ai21, alibaba, anthropic, 'ant-ling': antGroup, amazon: aws,
  'amazon-bedrock': bedrock, azure, cerebras, cloudflare, cohere, deepseek,
  fireworks, 'github-copilot': githubCopilot, google, groq, huggingface, ibm,
  meta, minimax, mistral, moonshotai: moonshot, nvidia, opencode, openai,
  openrouter, qwen, together, vercel, xai, xiaomi, zai, 'llama-cpp': llamaCpp,
  baidu, bytedance, dolphin, inflection, internlm, liquid, microsoft,
  nousresearch, perplexity, rwkv, skywork, stepfun, tencent, tii, upstage, yi,
};

// The map above encodes decisions — which brands get a colour variant, and
// which ids deliberately borrow another company's mark (ant-ling -> AntGroup,
// amazon -> AWS). Everything else falls through to whatever lobehub happens to
// ship under the same slug, so a lab we've never explicitly listed still gets
// its real logo instead of initials. Wordmark and alternate-lockup variants are
// excluded: they're wide, and these render in a square tile.
const ICONS = import.meta.glob<string>(
  [
    '../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-text.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-text-cn.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-brand.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-brand-color.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-combine.svg',
    '!../../../../../../../node_modules/@lobehub/icons-static-svg/icons/*-combine-color.svg',
  ],
  {query: '?url', import: 'default', eager: true},
);

const byIconName = new Map<string, string>();
for (const [path, url] of Object.entries(ICONS)) {
  byIconName.set(path.slice(path.lastIndexOf('/') + 1, -'.svg'.length), url);
}

/** Colour variant first, then the plain mark; `ant-ling` also tries `antling`
 * since lobehub's slugs drop punctuation. */
function iconBySlug(id: string): string | undefined {
  const bare = id.replace(/[^a-z0-9]/g, '');
  return byIconName.get(`${id}-color`) ?? byIconName.get(`${bare}-color`) ??
    byIconName.get(id) ?? byIconName.get(bare);
}

export function providerLogoUrl(provider: string): string | undefined {
  if (provider === 'amazon-bedrock') return bedrock;
  if (provider === 'azure-openai-responses') return azure;
  if (provider === 'google-vertex') return vertex;
  if (provider === 'cloudflare-workers-ai') return workers;
  if (provider.startsWith('cloudflare-')) return cloudflare;
  if (provider === 'kimi-coding' || provider.startsWith('moonshotai')) return moonshot;
  if (provider.startsWith('qwen-')) return qwen;
  if (provider.startsWith('minimax')) return minimax;
  if (provider.startsWith('zai')) return zai;
  if (provider.startsWith('xiaomi')) return xiaomi;
  if (provider.startsWith('opencode')) return opencode;
  if (provider === 'vercel-ai-gateway') return vercel;
  const id = companyId(provider);
  return logos[id] ?? iconBySlug(id);
}
