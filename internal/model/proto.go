package model

// Protoer is implemented by model types that can convert to a proto type P.
type Protoer[P any] interface {
	Proto() P
}

// ProtoMap converts a slice of models to a slice of their proto representations.
func ProtoMap[M Protoer[P], P any](models []M) []P {
	pb := make([]P, len(models))
	for i, m := range models {
		pb[i] = m.Proto()
	}
	return pb
}
