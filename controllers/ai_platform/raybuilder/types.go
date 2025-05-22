package raybuilder

// types.go
type ServeConfig struct {
	ProxyLocation string        `json:"proxy_location,omitempty"`
	HTTPOptions   HTTPOptions   `json:"http_options"`
	GRPCOptions   GRPCOptions   `json:"grpc_options"`
	LoggingConfig LoggingConfig `json:"logging_config"`
	Applications  []Application `json:"applications"`
}

type HTTPOptions struct {
	Host              string `json:"host,omitempty"`
	Port              int    `json:"port,omitempty"`
	RequestTimeoutS   int    `json:"request_timeout_s,omitempty"`
	KeepAliveTimeoutS int    `json:"keep_alive_timeout_s"`
}

type GRPCOptions struct {
	Port                  int      `json:"port,omitempty"`
	GRPCServicerFunctions []string `json:"grpc_servicer_functions,omitempty"`
	RequestTimeoutS       int      `json:"request_timeout_s,omitempty"`
}

type LoggingConfig struct {
	LogLevel        string `json:"log_level,omitempty"`
	LogsDir         string `json:"logs_dir,omitempty"`
	Encoding        string `json:"encoding,omitempty"`
	EnableAccessLog bool   `json:"enable_access_log,omitempty"`
}

// Application mirrors one rayService application
type Application struct {
	Name        string                 `json:"name"`
	ImportPath  string                 `json:"import_path,omitempty"`
	RoutePrefix string                 `json:"route_prefix,omitempty"`
	Args        map[string]interface{} `json:"args,omitempty"`
	RuntimeEnv  *RuntimeEnv            `json:"runtime_env,omitempty"`

	// catch any unmodeled keys:
	//Extras map[string]interface{} `json:",inline"`
}

// RuntimeEnv mirrors the runtime_env field in rayService
type RuntimeEnv struct {
	WorkingDir string            `json:"working_dir,omitempty"`
	EnvVars    map[string]string `json:"env_vars,omitempty"`
	Pip        []string          `json:"pip,omitempty"`
}

// Config is just a thin wrapper around rayService.applications
type Config struct {
	RayService ServeConfig `json:"rayService"`
}
