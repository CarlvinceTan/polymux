import { invoke, isTauri } from '../api/bridge';

export type LocationReading = {
  latitude: number;
  longitude: number;
  accuracyMeters: number;
  measuredAt: string;
  place: null;
  address: null;
};

type NativeReading = {
  latitude: number;
  longitude: number;
  accuracyMeters: number;
  measuredAt: number;
};

/** CoreLocation on macOS, the browser API elsewhere. */
export async function currentLocation(): Promise<LocationReading> {
  if (isTauri) {
    const value = await invoke<NativeReading>('current_location');
    return {
      latitude: value.latitude,
      longitude: value.longitude,
      accuracyMeters: value.accuracyMeters,
      measuredAt: new Date(value.measuredAt).toISOString(),
      place: null,
      address: null,
    };
  }
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error('Location is unavailable.'));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (position) => resolve({
        latitude: position.coords.latitude,
        longitude: position.coords.longitude,
        accuracyMeters: position.coords.accuracy,
        measuredAt: new Date(position.timestamp).toISOString(),
        place: null,
        address: null,
      }),
      (error) => reject(error),
      { enableHighAccuracy: true, timeout: 20000, maximumAge: 0 },
    );
  });
}
