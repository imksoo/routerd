// SPDX-License-Identifier: BSD-3-Clause

package main

func defaultConfigPath() string {
	return platformDefaults.ConfigFile()
}

func defaultLedgerPath() string {
	return platformDefaults.DBFile()
}

func defaultStatePath() string {
	return platformDefaults.DBFile()
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func defaultStatusSocketPath() string {
	return platformDefaults.StatusSocketFile()
}
