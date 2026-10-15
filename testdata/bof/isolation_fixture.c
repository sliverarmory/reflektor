#include <stdint.h>

#if defined(_WIN32)
#define BEACON_IMPORT __declspec(dllimport)
#else
#define BEACON_IMPORT
#endif

BEACON_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);

void go(char *buffer, int32_t length) {
    unsigned char binary[] = {0xff, 0x00, 0x80};
    BeaconOutput(0x1e, (char *)binary, (int32_t)sizeof(binary));
    BeaconOutput(-7, (char *)0, 0);

    if (length == 1 && buffer[0] == 'H') {
        volatile uint32_t running = 1;
        while (running) {
        }
    }
    if (length == 1 && buffer[0] == 'C') {
        __builtin_trap();
    }

    static char done[] = "worker-finished";
    BeaconOutput(0x20, done, (int32_t)(sizeof(done) - 1));
}
