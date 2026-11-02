import {polymuxApi} from '../../api/polymux';
import {readableError} from '../errors';
import {translate} from '../../../i18n';

export function createDictation(options: {
  getText: () => string;
  caret: () => number;
  setText: (text: string, caret: number) => void;
  autoStopSeconds: () => number | null;
  onState: (listening: boolean, error: string) => void;
}) {
  const api = polymuxApi();
  function notify() { options.onState(dictationListening, dictationError); }
  // Dictation writes into the draft; speech mode is the primary button instead.
  // Recording happens here, but recognition runs locally in the main process
  // (whisper.cpp): the Web Speech API needs Google's cloud recogniser, which
  // Electron does not ship, so it always failed mid-session.
  // The button no longer waits on any of it: pressing it settles the label in
  // the same tick, and each press owns a session so a transcript still landing
  // from the last one cannot write over the next.
  type Dictation = {
    /** Where this session's text sits in the draft — the caret at press time. */
    start: number;
    /** What the last pass wrote there, so the next one revises that span rather
        than appending a second copy of the same sentence. */
    written: string;
    chunks: Blob[];
    stream: MediaStream | null;
    recorder: MediaRecorder | null;
    /** Recording is over: stop feeding this session audio. */
    closed: boolean;
    /** The recorder flushed its last slice; release once the queue drains. */
    drained: boolean;
    pending: boolean;
    running: boolean;
    /** Tears down the silence watch; null when nothing is being watched. */
    listen: (() => void) | null;
  };
  /** How often a slice is cut and handed to a transcription pass. */
  const SLICE_INTERVAL = 600;
  /** How often the level is sampled while listening. */
  const LEVEL_INTERVAL = 100;
  /** Speech has to clear the room's own noise by this much, in dB. Rooms differ
   * far more than voices do, so the bar is set against a floor that follows the
   * room rather than at a fixed level. */
  const VOICE_MARGIN = 12;
  /** …but never treat the near-silence of a muted or dead mic as speech. */
  const VOICE_FLOOR = -55;
  /** The session taking audio, or null when the button reads VOICE. */
  let recording: Dictation | null = null;
  /** The newest session, recording or not — it alone may write to the draft. */
  let owner: Dictation | null = null;
  let dictationListening = false;
  let dictationError = '';

  /** Synchronous on purpose: the label and the ping settle in this tick, and
      the mic is acquired afterwards. */
  function toggleDictation(): void {
    if (dictationListening) {
      stopDictation();
      return;
    }
    dictationListening = true; notify();
    dictationError = ''; notify();
    const next: Dictation = {
      // Dictation adds to the composer, so it starts where the caret is and
      // leaves the text on either side of it alone.
      start: options.caret(),
      written: '',
      chunks: [],
      stream: null,
      recorder: null,
      closed: false,
      drained: false,
      pending: false,
      running: false,
      listen: null,
    };
    recording = next;
    owner = next;
    void openRecorder(next).catch(() => abandonRecorder(next, translate('dictation.startFailed')));
  }

  /** Hands the mic back and leaves the clip transcribing in the background, so
      the label returns to VOICE without waiting for whisper.cpp. */
  function stopDictation(): void {
    const session = recording;
    dictationListening = false; notify();
    recording = null;
    if (!session) return;
    endCapture(session);
    // No recorder yet means the mic never opened, so there is nothing to flush.
    if (session.recorder && session.recorder.state !== 'inactive') session.recorder.stop();
    else releaseRecorder(session);
    session.stream?.getTracks().forEach((track) => track.stop());
    session.stream = null;
  }

  /** Unmount path: drop the clip instead of transcribing it. */
  function cancelDictation(): void {
    const session = recording;
    owner = null;
    recording = null;
    dictationListening = false; notify();
    if (!session) return;
    endCapture(session);
    if (session.recorder) {
      session.recorder.onstop = null;
      if (session.recorder.state !== 'inactive') session.recorder.stop();
    }
    releaseRecorder(session);
  }

  /** Marks a session finished capturing: the watcher stops, and later audio and
      timers can no longer act on it. */
  function endCapture(session: Dictation): void {
    session.closed = true;
    session.listen?.();
    session.listen = null;
  }

  function releaseRecorder(session: Dictation): void {
    session.stream?.getTracks().forEach((track) => track.stop());
    session.stream = null;
    session.chunks = [];
  }

  async function openRecorder(session: Dictation): Promise<void> {
    const permission = await api.permissions.request('microphone');
    if (session.closed) return;
    if (permission !== 'granted') {
      abandonRecorder(session, translate('dictation.noPermission'));
      return;
    }
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({audio: true});
    } catch {
      abandonRecorder(session, translate('dictation.noMicrophone'));
      return;
    }
    session.stream = stream;
    // Stopped while the mic was being handed over: nothing was captured, so
    // drop the stream rather than record into a session nobody is watching.
    if (session.closed) {
      releaseRecorder(session);
      return;
    }
    const next = new MediaRecorder(stream);
    next.ondataavailable = (event) => {
      if (!event.data.size) return;
      session.chunks.push(event.data);
      void transcribeRecording(session);
    };
    next.onstop = () => {
      session.drained = true;
      void transcribeRecording(session);
    };
    session.recorder = next;
    // Each data slice extends the same WebM recording. Re-running local
    // Whisper over the accumulated clip lets the draft show partial results
    // without depending on a cloud streaming recogniser.
    //
    // The cadence is what dictation latency mostly is: a word spoken just after
    // a slice boundary waits this long before any pass can see it. Partials run
    // against a resident model in ~110ms, so the slice is the floor, not the
    // engine.
    next.start(SLICE_INTERVAL);
    try {
      watchForSilence(session, stream);
    } catch {
      // No level metering available: dictation still records, and the button
      // stays the way to end it.
    }
  }

  /** Stops listening once the room has been quiet for the configured window.
      Whatever was said before the silence is still transcribed, so this only
      spares the user from pressing the button again after they trail off. */
  function watchForSilence(session: Dictation, stream: MediaStream): void {
    const limit = options.autoStopSeconds();
    if (!limit) return;
    const context = new AudioContext();
    const analyser = context.createAnalyser();
    analyser.fftSize = 1024;
    const source = context.createMediaStreamSource(stream);
    source.connect(analyser);
    const samples = new Float32Array(analyser.fftSize);
    // Starts low so a genuinely quiet room does not have to shout to be heard,
    // and creeps up so a fan or a fridge starting mid-sentence re-baselines.
    let floor = -70;
    // Measured rather than counted in ticks: a throttled window fires the
    // interval late, and the user still expects the window they configured.
    let lastVoice = performance.now();
    const timer = setInterval(() => {
      analyser.getFloatTimeDomainData(samples);
      let sum = 0;
      for (const sample of samples) sum += sample * sample;
      const level = 20 * Math.log10(Math.sqrt(sum / samples.length) + 1e-9);
      floor = level < floor ? level : Math.min(floor + 0.15, level);
      if (level > Math.max(floor + VOICE_MARGIN, VOICE_FLOOR)) lastVoice = performance.now();
      else if (performance.now() - lastVoice >= limit * 1000 && recording === session) stopDictation();
    }, LEVEL_INTERVAL);
    session.listen = () => {
      clearInterval(timer);
      source.disconnect();
      void context.close();
    };
    void context.resume();
  }

  function abandonRecorder(session: Dictation, message: string): void {
    releaseRecorder(session);
    if (session.closed) return;
    endCapture(session);
    if (recording === session) {
      recording = null;
      dictationListening = false; notify();
    }
    if (owner === session) { dictationError = message; notify(); }
  }

  async function transcribeRecording(session: Dictation): Promise<void> {
    session.pending = true;
    if (session.running) return;
    session.running = true;
    while (session.pending) {
      session.pending = false;
      const clip = new Blob(session.chunks);
      if (!clip.size) continue;
      // The recorder has flushed, so this pass is the one whose text is kept:
      // it goes through the slower, more careful decode.
      const last = session.drained;
      try {
        const text = await api.dictation.transcribe(await monoWav(clip), last);
        // A later press owns the draft, so anything still arriving from this
        // clip would overwrite what that one is writing.
        if (text && owner === session) spliceTranscript(session, text);
      } catch (error) {
        if (owner === session) { dictationError = dictationFailure(error); notify(); }
      }
    }
    session.running = false;
    if (session.drained) releaseRecorder(session);
  }

  /**
   * Puts this pass's text where the session started, replacing only what the
   * previous pass wrote there. Everything the user typed survives — before the
   * span, after it, or while dictation was running.
   */
  function spliceTranscript(session: Dictation, text: string): void {
    const live = options.getText();
    let start = session.start;
    // Typing ahead of the span shifts it; find it again rather than write over
    // the characters now sitting at the old offset.
    if (live.slice(start, start + session.written.length) !== session.written) {
      const moved = live.indexOf(session.written);
      start = session.written && moved !== -1 ? moved : live.length;
      if (start === live.length) session.written = '';
    }
    const head = live.slice(0, start);
    const tail = live.slice(start + session.written.length);
    const lead = head && !/\s$/.test(head) ? ' ' : '';
    const trail = tail && !/^\s/.test(tail) ? ' ' : '';
    session.start = start;
    session.written = `${lead}${text}${trail}`;
    const draft = `${head}${session.written}${tail}`;
    // The caret belongs at the end of the dictated words, not after the text
    // that was already sitting to their right.
    options.setText(draft, head.length + lead.length + text.length);
  }

  /** whisper.cpp wants mono 16kHz 16-bit PCM; decodeAudioData resamples to the
   * context rate, so the conversion is one render plus a WAV header. */
  async function monoWav(clip: Blob): Promise<ArrayBuffer> {
    const context = new OfflineAudioContext(1, 1, 16000);
    const decoded = await context.decodeAudioData(await clip.arrayBuffer());
    const samples = decoded.getChannelData(0);
    const wav = new DataView(new ArrayBuffer(44 + samples.length * 2));
    const writeAscii = (offset: number, text: string) => {
      for (let index = 0; index < text.length; index += 1) wav.setUint8(offset + index, text.charCodeAt(index));
    };
    writeAscii(0, 'RIFF'); wav.setUint32(4, 36 + samples.length * 2, true); writeAscii(8, 'WAVE');
    writeAscii(12, 'fmt '); wav.setUint32(16, 16, true); wav.setUint16(20, 1, true); wav.setUint16(22, 1, true);
    wav.setUint32(24, 16000, true); wav.setUint32(28, 32000, true); wav.setUint16(32, 2, true); wav.setUint16(34, 16, true);
    writeAscii(36, 'data'); wav.setUint32(40, samples.length * 2, true);
    for (let index = 0; index < samples.length; index += 1) {
      const sample = Math.max(-1, Math.min(1, samples[index]));
      wav.setInt16(44 + index * 2, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
    }
    return wav.buffer;
  }

  function dictationFailure(error: unknown): string {
    const message = readableError(error);
    const detail = message.replace(/^Error invoking remote method '[^']+': (?:Error: )?/, '');
    return detail || translate('dictation.stopped');
  }

  return {toggle: toggleDictation, stop: stopDictation, cancel: cancelDictation};
}
