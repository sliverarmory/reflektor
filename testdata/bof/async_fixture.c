#include <stdint.h>

#if defined(_WIN32)
#define BEACON_IMPORT __declspec(dllimport)
#else
#define BEACON_IMPORT
#endif

BEACON_IMPORT void BeaconOutput(int32_t type, char *data, int32_t length);

// The host supplies a separate gate for each loaded image. Volatile forces a
// fresh read while the native entry point is running on its worker thread.
extern BEACON_IMPORT volatile uint32_t HostRelease;

void go(char *buffer, int32_t length) {
    if (length != 1 || (buffer[0] != 'A' && buffer[0] != 'B')) {
        static char invalid[] = "async-fixture-invalid-argument";
        BeaconOutput(13, invalid, (int32_t)(sizeof(invalid) - 1));
        return;
    }

    unsigned char started[3] = {'S', (unsigned char)buffer[0], 0xff};
    BeaconOutput(0x1e, (char *)started, (int32_t)sizeof(started));
    started[0] = 'X';
    started[1] = 'X';
    started[2] = 0;

    // This empty record confirms that the BOF has mutated the first record's
    // source buffer before the test checks ownership of the streamed bytes.
    BeaconOutput(0, (char *)0, 0);
    while (HostRelease == 0) {
    }

    unsigned char finished[3] = {'F', (unsigned char)buffer[0], 0};
    BeaconOutput(0x20, (char *)finished, (int32_t)sizeof(finished));
}
