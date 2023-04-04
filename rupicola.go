package rupicola

import "fmt"

func ListenAndServe(configuration Config) error {
	rupicolaProcessor := newRupicolaProcessorFromConfig(configuration)

	failureChannel := make(chan error)

	for idx := range configuration.Protocol.Bind {
		bind := configuration.Protocol.Bind[idx]
		child := rupicolaProcessor.spawnChild(bind)
		Logger.Info().Str("bind", fmt.Sprintf("%+v", bind)).Msg("Spawning worker")

		go func() {
			failureChannel <- child.listen()
		}()
	}
	Logger.Info().Msg("Listening for requests...")
	return <-failureChannel
}
