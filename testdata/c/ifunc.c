static int selected_implementation(void) {
	return 41;
}

static void *select_implementation(void) {
	return selected_implementation;
}

int ReflektorIFUNC(void) __attribute__((ifunc("select_implementation")));
__attribute__((visibility("hidden"))) int local_ifunc(void)
	__attribute__((ifunc("select_implementation")));

int ReflektorCallIFUNC(void) {
	return ReflektorIFUNC() + local_ifunc();
}
