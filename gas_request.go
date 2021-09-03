package main

import (
	"database/sql"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// GasRequest represents the json coming in from the request
type GasRequest struct {
	Network   string `json:"network"`
	Token     string `json:"discord_bot_token"`
	Nickname  bool   `json:"set_nickname"`
	Frequency int    `json:"frequency" default:"60"`
}

// AddTicker adds a new Ticker or crypto to the list of what to watch
func (m *Manager) AddGas(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()

	logger.Debugf("Got an API request to add a gas")

	// read body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Errorf("%s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// unmarshal into struct
	var gasReq GasRequest
	if err := json.Unmarshal(body, &gasReq); err != nil {
		logger.Errorf("Unmarshalling: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// ensure token is set
	if gasReq.Token == "" {
		logger.Error("Discord token required")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// ensure network is set
	if gasReq.Network == "" {
		logger.Error("Network required")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// check if already existing
	if _, ok := m.WatchingGas[strings.ToUpper(gasReq.Network)]; ok {
		logger.Error("Network already exists")
		w.WriteHeader(http.StatusConflict)
		return
	}

	gas := NewGas(gasReq.Network, gasReq.Token, gasReq.Nickname, gasReq.Frequency)
	m.addGas(gas)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(gas)
	if err != nil {
		logger.Errorf("Unable to encode gas: %s", err)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (m *Manager) addGas(gas *Gas) {
	gasCount.Inc()
	id := gas.Network
	m.WatchingGas[id] = gas

	var noDB *sql.DB
	if m.DB == noDB {
		return
	}

	// query
	stmt, err := m.DB.Prepare("SELECT id FROM gases WHERE network = ? LIMIT 1")
	if err != nil {
		logger.Warningf("Unable to query gas in db %s: %s", id, err)
		return
	}

	rows, err := stmt.Query(gas.Network)
	if err != nil {
		logger.Warningf("Unable to query gas in db %s: %s", id, err)
		return
	}

	var existingId int

	for rows.Next() {
		err = rows.Scan(&existingId)
		if err != nil {
			logger.Warningf("Unable to query gas in db %s: %s", id, err)
			return
		}
	}
	rows.Close()

	if existingId != 0 {

		// update entry in db
		stmt, err := m.DB.Prepare("update gases set token = ?, nickname = ?, network = ?, frequency = ? WHERE id = ?")
		if err != nil {
			logger.Warningf("Unable to update gas in db %s: %s", id, err)
			return
		}

		res, err := stmt.Exec(gas.token, gas.Nickname, gas.Network, gas.Frequency, existingId)
		if err != nil {
			logger.Warningf("Unable to update gas in db %s: %s", id, err)
			return
		}

		_, err = res.LastInsertId()
		if err != nil {
			logger.Warningf("Unable to update gas in db %s: %s", id, err)
			return
		}

		logger.Infof("Updated gas in db %s", id)
	} else {

		// store new entry in db
		stmt, err := m.DB.Prepare("INSERT INTO gases(token, nickname, network, frequency) values(?,?,?,?)")
		if err != nil {
			logger.Warningf("Unable to store gas in db %s: %s", id, err)
			return
		}

		res, err := stmt.Exec(gas.token, gas.Nickname, gas.Network, gas.Frequency)
		if err != nil {
			logger.Warningf("Unable to store gas in db %s: %s", id, err)
			return
		}

		_, err = res.LastInsertId()
		if err != nil {
			logger.Warningf("Unable to store gas in db %s: %s", id, err)
			return
		}
	}
}

// DeleteGas addds a new gas or crypto to the list of what to watch
func (m *Manager) DeleteGas(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()

	logger.Debugf("Got an API request to delete a gas")

	vars := mux.Vars(r)
	id := strings.ToUpper(vars["id"])

	if _, ok := m.WatchingGas[id]; !ok {
		logger.Errorf("No gas found: %s", id)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// send shutdown sign
	m.WatchingGas[id].Shutdown()
	gasCount.Dec()

	// remove from cache
	delete(m.WatchingGas, id)

	logger.Infof("Deleted gas %s", id)
	w.WriteHeader(http.StatusNoContent)
}

// GetGas returns a list of what the manager is watching
func (m *Manager) GetGas(w http.ResponseWriter, r *http.Request) {
	m.RLock()
	defer m.RUnlock()
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(m.WatchingGas); err != nil {
		logger.Errorf("Serving request: %s", err)
	}
}
