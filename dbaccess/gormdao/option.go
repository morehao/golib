package gormdao

type Option func(*config)

type config struct {
	isSoftDelete bool
}

func WithoutSoftDelete() Option {
	return func(c *config) {
		c.isSoftDelete = false
	}
}
