package rupicola

import "fmt"

func ListenAndServe(configuration *Config) error {
	rupicolaProcessor := newRupicolaProcessorFromConfig(configuration)

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		child := rupicolaProcessor.spawnChild(bind)
		Logger.Info().Str("bind", fmt.Sprintf("%+v", bind)).Msg("Spawning worker")
		go func() {
			failureChannel <- child.listen()
		}()
	}
	Logger.Info().Msg("Listening for requests...")
	return <-failureChannel
}
