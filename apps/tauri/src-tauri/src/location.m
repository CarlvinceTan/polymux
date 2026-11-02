#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>

typedef void (*LocationCallback)(const char *);

@interface PMLocation : NSObject <CLLocationManagerDelegate>
@property(nonatomic) LocationCallback callback;
@property(nonatomic, strong) CLLocationManager *manager;
@property(nonatomic) BOOL finished;
@end

static PMLocation *current;

@implementation PMLocation
- (void)emit:(NSDictionary *)value {
    if (!self.callback) return;
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    if (data) {
        NSString *json = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
        self.callback(json.UTF8String);
    }
    self.callback = NULL;
}

- (void)finish:(NSDictionary *)value {
    if (self.finished) return;
    self.finished = YES;
    [self.manager stopUpdatingLocation];
    self.manager.delegate = nil;
    self.manager = nil;
    if (current == self) current = nil;
    [self emit:value];
}

- (void)locationManagerDidChangeAuthorization:(CLLocationManager *)manager {
    CLAuthorizationStatus status = manager.authorizationStatus;
    if (status == kCLAuthorizationStatusNotDetermined) return;
    if (status == kCLAuthorizationStatusDenied || status == kCLAuthorizationStatusRestricted) {
        [self finish:@{ @"error": @"Allow Polymux to use your location in System Settings." }];
        return;
    }
    [manager requestLocation];
}

- (void)locationManager:(CLLocationManager *)manager didUpdateLocations:(NSArray<CLLocation *> *)locations {
    CLLocation *location = locations.lastObject;
    if (!location) return;
    [self finish:@{
        @"latitude": @(location.coordinate.latitude),
        @"longitude": @(location.coordinate.longitude),
        @"accuracyMeters": @(location.horizontalAccuracy),
        @"measuredAt": @(location.timestamp.timeIntervalSince1970 * 1000),
    }];
}

- (void)locationManager:(CLLocationManager *)manager didFailWithError:(NSError *)error {
    [self finish:@{ @"error": error.localizedDescription ?: @"Couldn’t get a location fix." }];
}

- (void)start {
    self.manager = [CLLocationManager new];
    self.manager.delegate = self;
    self.manager.desiredAccuracy = kCLLocationAccuracyBest;
    CLAuthorizationStatus status = self.manager.authorizationStatus;
    if (status == kCLAuthorizationStatusNotDetermined) {
        [self.manager requestWhenInUseAuthorization];
    } else if (status == kCLAuthorizationStatusDenied || status == kCLAuthorizationStatusRestricted) {
        [self finish:@{ @"error": @"Allow Polymux to use your location in System Settings." }];
    } else {
        [self.manager requestLocation];
    }
    __weak PMLocation *weak = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 25 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        PMLocation *owner = weak;
        if (!owner || owner.finished) return;
        [owner finish:@{ @"error": @"Couldn’t get a location fix." }];
    });
}
@end

void polymux_location_start(LocationCallback callback) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (current) {
            callback("{\"error\":\"Location is already active.\"}");
            return;
        }
        PMLocation *value = [PMLocation new];
        value.callback = callback;
        current = value;
        [value start];
    });
}
