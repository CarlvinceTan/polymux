#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#import <Speech/Speech.h>

typedef void (*DictationCallback)(const char *);

@interface PMDictation : NSObject
@property(nonatomic) uint64_t identifier;
@property(nonatomic) DictationCallback callback;
@property(nonatomic, strong) AVAudioEngine *engine;
@property(nonatomic, strong) SFSpeechRecognizer *recognizer;
@property(nonatomic, strong) SFSpeechAudioBufferRecognitionRequest *request;
@property(nonatomic, strong) SFSpeechRecognitionTask *task;
@property(nonatomic, strong) NSLock *lock;
@property(nonatomic) uint64_t generation;
@property(nonatomic) double segment;
@property(nonatomic) BOOL tapped;
@property(nonatomic) BOOL stopping;
@property(nonatomic) NSTimeInterval lastLevel;
@end

static PMDictation *current;

@implementation PMDictation
- (void)emit:(NSString *)kind fields:(NSDictionary *)fields {
    NSMutableDictionary *value = [fields mutableCopy];
    value[@"kind"] = kind;
    value[@"id"] = @(self.identifier);
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    if (data && self.callback) {
        NSString *json = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
        self.callback(json.UTF8String);
    }
}

- (void)finish {
    if (current != self) return;
    self.generation++;
    [self.engine stop];
    if (self.tapped) [self.engine.inputNode removeTapOnBus:0];
    self.tapped = NO;
    [self.lock lock];
    [self.request endAudio];
    self.request = nil;
    [self.lock unlock];
    [self.task cancel];
    self.task = nil;
    [self emit:@"ended" fields:@{}];
    current = nil;
}

- (void)fail:(BOOL)denied {
    if (current != self) return;
    [self emit:@"error" fields:@{ @"denied": @(denied) }];
    [self finish];
}

- (void)recognize {
    if (current != self || self.stopping) return;
    self.generation++;
    uint64_t generation = self.generation;
    double segment = self.segment;
    SFSpeechAudioBufferRecognitionRequest *request = [SFSpeechAudioBufferRecognitionRequest new];
    request.shouldReportPartialResults = YES;
    request.requiresOnDeviceRecognition = self.recognizer.supportsOnDeviceRecognition;
    request.contextualStrings = @[@"Polymux"];
    request.taskHint = SFSpeechRecognitionTaskHintDictation;
    request.addsPunctuation = YES;
    [self.lock lock];
    self.request = request;
    [self.lock unlock];
    __weak PMDictation *weak = self;
    self.task = [self.recognizer recognitionTaskWithRequest:request resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
        dispatch_async(dispatch_get_main_queue(), ^{
            PMDictation *owner = weak;
            if (!owner || current != owner || owner.generation != generation) return;
            if (result) {
                [owner emit:@"transcript" fields:@{ @"text": result.bestTranscription.formattedString,
                    @"start": @(segment), @"final": @(result.isFinal) }];
                if (result.isFinal) {
                    if (owner.stopping) { [owner finish]; return; }
                    owner.segment += 1;
                    [owner recognize];
                    return;
                }
            }
            if (error) {
                if (owner.stopping) { [owner finish]; return; }
                if ([error.domain isEqualToString:@"kAFAssistantErrorDomain"] && error.code == 1110) {
                    owner.segment += 1;
                    [owner recognize];
                } else if ([error.domain isEqualToString:@"kLSRErrorDomain"] && error.code == 201) {
                    [owner emit:@"error" fields:@{ @"denied": @NO, @"message": @"Turn on Dictation in System Settings." }];
                    [owner finish];
                } else [owner fail:NO];
            }
        });
    }];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 55 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        PMDictation *owner = weak;
        if (!owner || current != owner || owner.stopping || owner.generation != generation) return;
        [owner.task cancel];
        [owner.lock lock];
        [owner.request endAudio];
        [owner.lock unlock];
        owner.segment += 1;
        [owner recognize];
    });
}

- (void)capture {
    if (current != self) return;
    self.recognizer = [[SFSpeechRecognizer alloc] initWithLocale:NSLocale.currentLocale];
    if (!self.recognizer || !self.recognizer.available) { [self fail:NO]; return; }
    self.lock = [NSLock new];
    self.engine = [AVAudioEngine new];
    AVAudioInputNode *input = self.engine.inputNode;
    AVAudioFormat *format = [input outputFormatForBus:0];
    if (format.sampleRate <= 0 || format.channelCount == 0) { [self fail:NO]; return; }
    [self recognize];
    __weak PMDictation *weak = self;
    AVAudioNodeTapBlock tap = ^(AVAudioPCMBuffer *buffer, AVAudioTime *time) {
        (void)time;
        PMDictation *owner = weak;
        if (!owner) return;
        [owner.lock lock];
        [owner.request appendAudioPCMBuffer:buffer];
        [owner.lock unlock];
        NSTimeInterval now = NSProcessInfo.processInfo.systemUptime;
        if (now - owner.lastLevel < .04 || !buffer.floatChannelData || !buffer.frameLength) return;
        owner.lastLevel = now;
        float sum = 0;
        for (AVAudioFrameCount i = 0; i < buffer.frameLength; i++) sum += buffer.floatChannelData[0][i] * buffer.floatChannelData[0][i];
        float level = fminf(1, sqrtf(sum / buffer.frameLength) * 12);
        dispatch_async(dispatch_get_main_queue(), ^{ if (current == owner) [owner emit:@"level" fields:@{ @"level": @(level) }]; });
    };
    if (@available(macOS 27.0, *)) {
        NSError *tapError = nil;
        if (![input installTapOnBus:0 bufferSize:1024 format:format error:&tapError block:tap]) { [self fail:NO]; return; }
    } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        [input installTapOnBus:0 bufferSize:1024 format:format block:tap];
#pragma clang diagnostic pop
    }
    self.tapped = YES;
    [self.engine prepare];
    NSError *error = nil;
    if (![self.engine startAndReturnError:&error]) { [self fail:NO]; return; }
    [self emit:@"ready" fields:@{}];
}

- (void)start {
    __weak PMDictation *weak = self;
    [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL allowed) {
        dispatch_async(dispatch_get_main_queue(), ^{
            PMDictation *owner = weak;
            if (!owner || current != owner) return;
            if (!allowed) { [owner fail:YES]; return; }
            [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus status) {
                dispatch_async(dispatch_get_main_queue(), ^{
                    PMDictation *owner = weak;
                    if (!owner || current != owner) return;
                    if (status != SFSpeechRecognizerAuthorizationStatusAuthorized) { [owner fail:YES]; return; }
                    [owner capture];
                });
            }];
        });
    }];
}

- (void)stop:(BOOL)finalize {
    if (current != self) return;
    if (!finalize || !self.engine) { [self finish]; return; }
    if (self.stopping) return;
    self.stopping = YES;
    [self.engine stop];
    if (self.tapped) [self.engine.inputNode removeTapOnBus:0];
    self.tapped = NO;
    [self.lock lock];
    [self.request endAudio];
    [self.lock unlock];
    __weak PMDictation *weak = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), dispatch_get_main_queue(), ^{ [weak finish]; });
}
@end

void polymux_dictation_start(uint64_t identifier, DictationCallback callback) {
    dispatch_async(dispatch_get_main_queue(), ^{
        PMDictation *value = [PMDictation new];
        value.identifier = identifier;
        value.callback = callback;
        if (current) { [value emit:@"error" fields:@{ @"denied": @NO }]; [value emit:@"ended" fields:@{}]; return; }
        current = value;
        [value start];
    });
}

void polymux_dictation_stop(uint64_t identifier, bool finalize) {
    dispatch_async(dispatch_get_main_queue(), ^{ if (current.identifier == identifier) [current stop:finalize]; });
}
