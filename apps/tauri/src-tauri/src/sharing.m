#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <QuickLookUI/QuickLookUI.h>

@interface PMPreview : NSObject <QLPreviewPanelDataSource, QLPreviewPanelDelegate>
@property(nonatomic, copy) NSArray<NSURL *> *urls;
@end

static PMPreview *preview;
static NSSharingServicePicker *sharing;

@implementation PMPreview
- (NSInteger)numberOfPreviewItemsInPreviewPanel:(QLPreviewPanel *)panel {
    return self.urls.count;
}
- (id<QLPreviewItem>)previewPanel:(QLPreviewPanel *)panel previewItemAtIndex:(NSInteger)index {
    return self.urls[index];
}
@end

void pm_quick_look(const char *path) {
    if (!path) return;
    NSString *value = [NSString stringWithUTF8String:path];
    if (!value.length) return;
    NSURL *url = [NSURL fileURLWithPath:value];
    dispatch_async(dispatch_get_main_queue(), ^{
        preview = [PMPreview new];
        preview.urls = @[url];
        QLPreviewPanel *panel = [QLPreviewPanel sharedPreviewPanel];
        panel.dataSource = preview;
        panel.delegate = preview;
        [panel reloadData];
        [panel makeKeyAndOrderFront:nil];
    });
}

void pm_open_file(const char *path) {
    if (!path) return;
    NSString *value = [NSString stringWithUTF8String:path];
    if (!value.length) return;
    NSURL *url = [NSURL fileURLWithPath:value];
    dispatch_async(dispatch_get_main_queue(), ^{
        [[NSWorkspace sharedWorkspace] openURL:url];
    });
}

void pm_share_file(const char *path) {
    if (!path) return;
    NSString *value = [NSString stringWithUTF8String:path];
    if (!value.length) return;
    NSURL *url = [NSURL fileURLWithPath:value];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = NSApp.mainWindow ?: NSApp.keyWindow;
        NSView *anchor = window.contentView;
        if (!anchor) return;
        sharing = [[NSSharingServicePicker alloc] initWithItems:@[url]];
        NSRect rect = NSMakeRect(anchor.bounds.size.width / 2, anchor.bounds.size.height / 2, 1, 1);
        [sharing showRelativeToRect:rect ofView:anchor preferredEdge:NSRectEdgeMinY];
    });
}
