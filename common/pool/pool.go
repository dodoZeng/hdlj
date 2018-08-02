package pool

type IterateFun func(interface{}) (bool, string)

type Pool interface {
	Get() (interface{}, error)

	Put(interface{}) error

	Iterate(IterateFun) (bool, string)

	Len() int
}
