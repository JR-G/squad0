package agent

// SessionEnvForTest exposes sessionEnv for external tests.
func SessionEnvForTest(agent *Agent) map[string]string {
	return agent.sessionEnv()
}
