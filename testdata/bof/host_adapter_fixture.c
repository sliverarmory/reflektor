#include <stdint.h>

#if defined(_WIN32)
#define BEACON_IMPORT __declspec(dllimport)
#else
#define BEACON_IMPORT
#endif

BEACON_IMPORT int32_t BeaconUseToken(uintptr_t token);
BEACON_IMPORT void BeaconRevertToken(void);
BEACON_IMPORT int32_t BeaconIsAdmin(void);
BEACON_IMPORT void BeaconGetSpawnTo(int32_t x86, char *buffer, int32_t length);
BEACON_IMPORT void BeaconOutput(int32_t type, const char *data, int32_t length);

void go(char *args, int32_t length) {
    char path[32] = {0};
    static const char success[] = "host-adapter-ok";
    static const char failure[] = "host-adapter-failed";
    (void)args;
    if (!BeaconUseToken((uintptr_t)0x345) || !BeaconIsAdmin()) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    if (length == 2 && !BeaconUseToken((uintptr_t)0x345)) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    BeaconGetSpawnTo(1, path, (int32_t)sizeof(path));
    if (length != 1 && length != 2) {
        BeaconRevertToken();
    }
    if (path[0] != 'h' || path[1] != 'o' || path[2] != 's' ||
        path[3] != 't' || path[4] != 0) {
        BeaconOutput(13, failure, (int32_t)(sizeof(failure) - 1));
        return;
    }
    BeaconOutput(0, success, (int32_t)(sizeof(success) - 1));
}
