#include <stdint.h>

typedef struct {
    char *original;
    char *buffer;
    int32_t length;
    int32_t size;
} formatp;

extern void BeaconPrintf(int32_t type, const char *format, ...);
extern void BeaconFormatAlloc(formatp *format, int32_t size);
extern void BeaconFormatPrintf(formatp *format, const char *string, ...);
extern char *BeaconFormatToString(formatp *format, int32_t *size);
extern void BeaconFormatFree(formatp *format);
extern void BeaconOutput(int32_t type, char *data, int32_t length);

// The volatile function pointer forces an address relocation in addition to
// the direct-call relocation. Both must resolve to the Apple ABI bridge.
static void (*volatile indirect_printf)(int32_t, const char *, ...);

void go(char *buffer, int32_t length) {
    (void)buffer;
    (void)length;
    BeaconPrintf(0, "stack=%d:%s:%llx:%d:%d:%d:%d:%d:%d:%d",
                 7, "ok", 0x1122334455667788ULL, 4, 5, 6, 7, 8, 9, 10);
    indirect_printf = BeaconPrintf;
    indirect_printf(0, "indirect=%d:%s", -3, "ok");
    BeaconPrintf(0, "no-args");
    BeaconPrintf(0, "float=%.1f", 1.5);

    formatp format = {0};
    int32_t size = 0;
    BeaconFormatAlloc(&format, 256);
    BeaconFormatPrintf(&format, "format=%d:%s:%d:%d:%d:%d:%d:%d:%d:%d",
                       1, "ok", 3, 4, 5, 6, 7, 8, 9, 10);
    BeaconFormatPrintf(&format, ":tail");
    char *data = BeaconFormatToString(&format, &size);
    BeaconOutput(0, data, size);
    BeaconFormatFree(&format);
}
