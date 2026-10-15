#include <stdint.h>

#if defined(_WIN32)
#define BOF_IMPORT __declspec(dllimport)
#else
#define BOF_IMPORT
#endif

BOF_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);
BOF_IMPORT int ReflektorStartWorker(void (*worker)(char *, int32_t), const char *data, int32_t length);
extern BOF_IMPORT volatile uint32_t HostRelease;

static void worker(char *data, int32_t length) {
    char ready[] = "worker-ready";
    BeaconOutput(1, ready, (int32_t)(sizeof(ready) - 1));
    while (HostRelease == 0) {
    }
    if (length == 1) {
        if (data[0] == 'A') {
            volatile char child = 'B';
            if (!ReflektorStartWorker(worker, (const char *)&child, 1)) {
                char failed[] = "child-failed";
                BeaconOutput(6, failed, (int32_t)(sizeof(failed) - 1));
            }
            child = 'X';
        }
        BeaconOutput(2, data, length);
    }
    char done[] = "worker-done";
    BeaconOutput(3, done, (int32_t)(sizeof(done) - 1));
}

void go(char *buffer, int32_t length) {
    (void)buffer;
    (void)length;
    volatile char copied = 'A';
    int started = ReflektorStartWorker(worker, (const char *)&copied, 1);
    copied = 'X';
    if (started) {
        char returned[] = "entry-returned";
        BeaconOutput(4, returned, (int32_t)(sizeof(returned) - 1));
    } else {
        char disabled[] = "worker-disabled";
        BeaconOutput(5, disabled, (int32_t)(sizeof(disabled) - 1));
    }
}
