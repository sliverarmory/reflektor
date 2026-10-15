static __thread int reflektor_tls_value = 7;

int ReflektorTLSValue(void) {
	return ++reflektor_tls_value;
}
