package types

type GoldenTest struct {
	Args []string
	Env  map[string]string
	Plan string
}

type GoldenTests struct {
	Name       string
	Dockerfile string
	Tests      []GoldenTest
}
