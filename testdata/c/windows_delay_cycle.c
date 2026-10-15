#if !defined(_WIN32)
#error Windows delay-cycle fixture
#endif

#define DLL_PROCESS_ATTACH 1
#define DLL_PROCESS_DETACH 0
#define EXPORT __declspec(dllexport)
#define IMPORT __declspec(dllimport)

#if defined(REFLEKTOR_DELAY_CYCLE_ROOT)

IMPORT int DelayCyclePeerValue(void);
IMPORT void DelayCyclePeerSetObserver(volatile int *record);

static int root_state;
static volatile int *observer;

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    root_state = DelayCyclePeerValue();
  } else if (reason == DLL_PROCESS_DETACH && observer != 0) {
    if (DelayCyclePeerValue() != 41) observer[4] = -1;
    observer[0] = ++observer[3];
  }
  return 1;
}

EXPORT int DelayCycleRootValue(void) { return 40; }
EXPORT int DelayCycleResult(void) { return root_state + 1; }
EXPORT void DelayCycleSetObserver(volatile int *record) {
  observer = record;
  DelayCyclePeerSetObserver(record);
}

#elif defined(REFLEKTOR_DELAY_CYCLE_PEER)

IMPORT int DelayCycleForwardedRootValue(void);
IMPORT int DelayCycleForwarderReady(void);
IMPORT void DelayCycleForwarderSetObserver(volatile int *record);

static int peer_state;
static volatile int *observer;

// Recursive loading binds the delay IAT before any DllMain runs. The helper
// satisfies the linker and must never be reached by this fixture.
void *__stdcall __delayLoadHelper2(void *descriptor, void **slot) {
  (void)descriptor;
  (void)slot;
  return 0;
}

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    peer_state = DelayCycleForwardedRootValue() + DelayCycleForwarderReady();
  } else if (reason == DLL_PROCESS_DETACH && observer != 0) {
    if (DelayCycleForwardedRootValue() != 40) observer[4] = -2;
    observer[1] = ++observer[3];
  }
  return 1;
}

EXPORT int DelayCyclePeerValue(void) { return peer_state; }
EXPORT void DelayCyclePeerSetObserver(volatile int *record) {
  observer = record;
  DelayCycleForwarderSetObserver(record);
}

#elif defined(REFLEKTOR_DELAY_CYCLE_FORWARDER)

static int forwarder_state;
static volatile int *observer;

int __stdcall DllMain(void *instance, unsigned long reason, void *reserved) {
  (void)instance;
  (void)reserved;
  if (reason == DLL_PROCESS_ATTACH) {
    forwarder_state = 1;
  } else if (reason == DLL_PROCESS_DETACH && observer != 0) {
    observer[2] = ++observer[3];
  }
  return 1;
}

EXPORT int DelayCycleForwarderReady(void) { return forwarder_state; }
EXPORT void DelayCycleForwarderSetObserver(volatile int *record) {
  observer = record;
}

#else
#error Select a delay-cycle fixture variant.
#endif
