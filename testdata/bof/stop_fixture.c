#include <stdint.h>

#if defined(_WIN32)
#define BOF_IMPORT __declspec(dllimport)
#else
#define BOF_IMPORT
#endif

BOF_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);
BOF_IMPORT int ReflektorShouldStop(void);

void go(char *buffer, int32_t length) {
    char ready[] = "ready";
    BeaconOutput(0, ready, (int32_t)(sizeof(ready) - 1));

    while (!ReflektorShouldStop()) {
    }

    char stopped[] = "stopped";
    BeaconOutput(0, stopped, (int32_t)(sizeof(stopped) - 1));
}
