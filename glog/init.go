package glog

func init() {
	logger, err := getDefaultLogger()
	if err != nil {
		return
	}
	defaultLogger = logger
}
