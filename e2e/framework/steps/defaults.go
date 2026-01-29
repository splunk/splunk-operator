package steps

// RegisterDefaults registers all built-in handlers.
func RegisterDefaults(reg *Registry) {
	RegisterDataHandlers(reg)
	RegisterTopologyHandlers(reg)
	RegisterSplunkdHandlers(reg)
	RegisterClusterHandlers(reg)
	RegisterK8sHandlers(reg)
	RegisterLicenseHandlers(reg)
	RegisterSecretHandlers(reg)
	RegisterPhaseHandlers(reg)
	RegisterMiscHandlers(reg)
	RegisterObjectstoreHandlers(reg)
	RegisterAppFrameworkHandlers(reg)
}
