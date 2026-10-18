import bluesky from '../assets/platforms/bluesky.svg?url';
import fastmail from '../assets/platforms/fastmail.svg?url';
import gmail from '../assets/platforms/gmail.svg?url';
import gmessages from '../assets/platforms/gmessages.svg?url';
import googlechat from '../assets/platforms/googlechat.svg?url';
import gvoice from '../assets/platforms/gvoice.svg?url';
import icloud from '../assets/platforms/icloud.svg?url';
import imessage from '../assets/platforms/imessage.svg?url';
import instagram from '../assets/platforms/instagram.svg?url';
import lark from '../assets/platforms/lark.png';
import linkedin from '../assets/platforms/linkedin.svg?url';
import mail from '../assets/platforms/mail.png';
import matrix from '../assets/platforms/matrix.svg?url';
import messenger from '../assets/platforms/messenger.svg?url';
import outlook from '../assets/platforms/outlook.svg?url';
import signal from '../assets/platforms/signal.svg?url';
import slack from '../assets/platforms/slack.svg?url';
import telegram from '../assets/platforms/telegram.svg?url';
import twitter from '../assets/platforms/twitter.svg?url';
import wechat from '../assets/platforms/wechat.svg?url';
import whatsapp from '../assets/platforms/whatsapp.svg?url';
import zulip from '../assets/platforms/zulip.svg?url';

const bridgeLogos: Record<string, string> = {
  whatsapp,
  telegram,
  signal,
  slack,
  messenger,
  instagram,
  linkedin,
  googlechat,
  gmessages,
  twitter,
  bluesky,
  gvoice,
  zulip,
  imessage,
  wechat,
  matrix,
};

const mailLogos: Record<string, string> = {
  gmail,
  outlook,
  icloud,
  lark,
  fastmail,
  custom: mail,
};

export function bridgeLogo(platform: string): string | null {
  return bridgeLogos[platform] ?? null;
}

export function mailLogo(preset: string): string | null {
  return mailLogos[preset] ?? null;
}
