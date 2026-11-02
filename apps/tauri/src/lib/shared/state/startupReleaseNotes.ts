const LAST_RELEASE_NOTES_VERSION_KEY = 'polymux:last-release-notes-version';
const RELEASES_URL = 'https://polymux.com/releases/';

type VersionStorage = Pick<Storage, 'getItem' | 'setItem'>;

export type StartupReleaseNotes = {
  title: string;
  url: string;
  version: string;
};

function numericVersion(version: string): number[] | null {
  const core = version.trim().replace(/^v/, '').split(/[+-]/, 1)[0];
  if (!/^\d+(?:\.\d+)+$/.test(core)) return null;
  return core.split('.').map(Number);
}

function isNewerVersion(current: string, previous: string): boolean {
  const currentParts = numericVersion(current);
  const previousParts = numericVersion(previous);
  if (!currentParts || !previousParts) return current !== previous;

  const length = Math.max(currentParts.length, previousParts.length);
  for (let index = 0; index < length; index += 1) {
    const difference = (currentParts[index] ?? 0) - (previousParts[index] ?? 0);
    if (difference !== 0) return difference > 0;
  }
  return false;
}

/**
 * Records every launch immediately, then returns a release page only when an
 * existing installation has moved to a newer app version. A user still in
 * onboarding establishes a baseline without seeing release notes.
 */
export function startupReleaseNotes(
  storage: VersionStorage,
  currentVersion: string,
  onboardingCompleted: boolean,
): StartupReleaseNotes | null {
  const version = currentVersion.trim().replace(/^v/, '');
  if (!version) return null;

  try {
    const previousVersion = storage.getItem(LAST_RELEASE_NOTES_VERSION_KEY);
    storage.setItem(LAST_RELEASE_NOTES_VERSION_KEY, version);

    if (!onboardingCompleted || previousVersion === version) return null;
    if (previousVersion !== null && !isNewerVersion(version, previousVersion)) return null;

    return {
      version,
      title: `Polymux ${version} release notes`,
      url: `${RELEASES_URL}${encodeURIComponent(version)}/`,
    };
  } catch {
    return null;
  }
}
