package gormdao

type Option func(*options)

type options struct {
	isSoftDelete bool
}

func WithoutSoftDelete() Option {
	return func(c *options) {
		c.isSoftDelete = false
	}
}
