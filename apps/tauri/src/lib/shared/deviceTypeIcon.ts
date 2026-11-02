import type {DeviceType} from '@polymux/protocol';

export type DeviceIconName = 'computer' | 'laptop' | 'tablet' | 'phone' | 'server';

/**
 * The glyph a device kind is drawn with, wherever a Host is named.
 *
 * A desktop PC and a Host that has not reported its kind share the generic
 * computer glyph, so the name beside it always keeps its mark.
 */
export function deviceTypeIconName(type: DeviceType | undefined): DeviceIconName {
  return !type || type === 'pc' ? 'computer' : type;
}
