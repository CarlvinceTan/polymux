import bluesky from '../../../assets/platforms/bluesky.svg?url';
import fastmail from '../../../assets/platforms/fastmail.svg?url';
import gmail from '../../../assets/platforms/gmail.svg?url';
import gmessages from '../../../assets/platforms/gmessages.svg?url';
import googlechat from '../../../assets/platforms/googlechat.svg?url';
import gvoice from '../../../assets/platforms/gvoice.svg?url';
import icloud from '../../../assets/platforms/icloud.svg?url';
import imessage from '../../../assets/platforms/imessage.svg?url';
import instagram from '../../../assets/platforms/instagram.svg?url';
import lark from '../../../assets/platforms/lark.png';
import linkedin from '../../../assets/platforms/linkedin.svg?url';
import mail from '../../../assets/platforms/mail.png';
import matrix from '../../../assets/platforms/matrix.svg?url';
import messenger from '../../../assets/platforms/messenger.svg?url';
import outlook from '../../../assets/platforms/outlook.svg?url';
import signal from '../../../assets/platforms/signal.svg?url';
import slack from '../../../assets/platforms/slack.svg?url';
import telegram from '../../../assets/platforms/telegram.svg?url';
import twitter from '../../../assets/platforms/twitter.svg?url';
import wechat from '../../../assets/platforms/wechat.svg?url';
import whatsapp from '../../../assets/platforms/whatsapp.svg?url';
import zulip from '../../../assets/platforms/zulip.svg?url';

/**
 * Each company's own mark, in full colour, keyed the way the bridge fleet and
 * the mail presets name them. The files live in the bundle rather than being
 * fetched: setup has to work on a machine that is not online yet.
 *
 * This covers every platform the bridge fleet can report and every mail preset
 * we offer, so no seat falls back to a lettered disc.
 */
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
  // Any other IMAP server: macOS Mail's own icon, taken from this Mac, so it
  // always matches the system the user is looking at.
  custom: mail,
};

/** The logo for a messaging bridge, by its platform id. */
export function bridgeLogo(platform: string): string | null {
  return bridgeLogos[platform] ?? null;
}

/** The logo for a mail provider, by its preset value. */
export function mailLogo(preset: string): string | null {
  return mailLogos[preset] ?? null;
}
