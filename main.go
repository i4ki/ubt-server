package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

type (
	Character  int
	StatusCode int
	State      int

	playerName string

	Player struct {
		Name      string    `json:"name"`
		ipaddr    string    `json:"ipaddr,omitempty"`
		Character Character `json:"character,omitempty"`
		Left      bool      `json:"left",omitempty"`
		Keys      chan string

		state State
	}

	GameState struct {
		players map[playerName]*Player
		sync.Mutex
	}

	statusMessage struct {
		Status string     `json:"status"`
		Code   StatusCode `json:"code"`
	}

	selectMessage struct {
		Character string     `json:"character"`
		Player    string     `json:"player"`
		Status    string     `json:"status"`
		Code      StatusCode `json:"code"`
	}

	connectMessage struct {
		Status string     `json:"status"`
		Code   StatusCode `json:"code"`
		Left   bool       `json:"left"`
	}

	getInfoMessage struct {
		Player playerName `json:"player"`
		Action string     `json:"action"`
	}
)

const (
	Max Character = iota + 1
	Drax
)

const (
	stateSelect State = iota + 1
	stateWaitPlayers
	stateSync
	statePlay
)

const (
	ESuccess StatusCode = iota + 1
	ETooManyPlayers
	ESelect
	ENotReady
	EInternal
)

func (c Character) String() string {
	if c == Max {
		return "max"
	}

	if c == Drax {
		return "drax"
	}

	return "<unknown>"
}

func main() {
	l, err := net.Listen("tcp", ":5000")

	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}

	defer l.Close()

	fmt.Println("Listening on :5000")

	game := GameState{
		Mutex:   sync.Mutex{},
		players: make(map[playerName]*Player),
	}

	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}

		go handleRequest(conn, &game)
	}
}

func toManyPlayers(conn net.Conn) {
	errMsg := statusMessage{
		Status: "Too many players in the room",
		Code:   ETooManyPlayers,
	}

	msg, err := json.Marshal(errMsg)

	if err != nil {
		conn.Write([]byte("internal error. Failed to marshal json"))
		return
	}

	conn.Write(msg)
}

func sendMessage(conn net.Conn, message interface{}) error {
	data, err := json.Marshal(&message)

	if err != nil {
		return err
	}

	_, err = conn.Write(append(data, '\n'))

	fmt.Printf("Sent message: %s\n", string(data))

	return err
}

func sendSuccess(conn net.Conn) error {
	connSuccess := statusMessage{
		Status: "connection successfully",
		Code:   ESuccess,
	}

	return sendMessage(conn, connSuccess)
}

func sendNotReady(conn net.Conn) error {
	notReady := statusMessage{
		Status: "not ready",
		Code:   ENotReady,
	}

	return sendMessage(conn, notReady)
}

func sendInternalError(conn net.Conn) error {
	errInternal := statusMessage{
		Status: "internal error",
		Code:   EInternal,
	}

	return sendMessage(conn, errInternal)
}

func sendConnectSuccess(conn net.Conn, left bool) error {
	connMessage := connectMessage{
		Status: "Connected successfully",
		Code:   ESuccess,
		Left:   left,
	}

	return sendMessage(conn, connMessage)
}

func sendSelectedPlayer(conn net.Conn, player *Player) error {
	selectedMsg := selectMessage{
		Player:    player.Name,
		Character: player.Character.String(),
		Status:    "Other player ready",
		Code:      ESuccess,
	}

	return sendMessage(conn, selectedMsg)
}

func readMessage(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)

	if err != nil {
		return nil, err
	}

	fmt.Printf("Received: %s\n", string(buf[:n]))

	return buf[:n], nil
}

func getPlayer(conn net.Conn) (Player, error) {
	buf, err := readMessage(conn)

	if err != nil {
		return Player{}, err
	}

	var player Player
	err = json.Unmarshal(buf, &player)

	player.Keys = make(chan string, 16)

	if err != nil {
		return Player{}, err
	}

	return player, nil
}

func getPlayerCharSelect(conn net.Conn) (Character, error) {
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)

	if err != nil {
		return 0, err
	}

	fmt.Printf("Received: %s\n", string(buf[:n]))

	var message selectMessage
	err = json.Unmarshal(buf[:n], &message)

	if err != nil {
		return 0, err
	}

	if message.Character == "" {
		return 0, errors.New("Empty character name")
	}

	if message.Character == "drax" {
		return Drax, nil
	} else if message.Character == "max" {
		return Max, nil
	}

	return 0, errors.New("invalid character choice")
}

func sendOtherPlayerKey(conn net.Conn, key string) error {
	dataStr := `{"key": "` + key + `"}`

	fmt.Printf("Sending info: %s\n", dataStr)

	_, err := conn.Write([]byte(dataStr + "\n"))

	if err != nil {
		return err
	}

	return nil
}

func getPlayerKey(conn net.Conn) (string, error) {
	data, err := readMessage(conn)

	if err != nil {
		return "", err
	}

	var info map[string]string

	err = json.Unmarshal(data, &info)

	if err != nil {
		return "", err
	}

	return info["key"], nil
}

func handleRequest(conn net.Conn, game *GameState) {
	fmt.Printf("Connection established from %s\n", conn.RemoteAddr())

	defer conn.Close()

	game.Lock()

	if len(game.players) == 2 {
		game.Unlock()

		data, err := readMessage(conn)

		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
			return
		}

		var dataObj getInfoMessage

		err = json.Unmarshal(data, &dataObj)

		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
			return
		}

		player, ok := game.players[dataObj.Player]

		if !ok {
			fmt.Fprintf(os.Stderr, "ERROR: Player not found\n")
			return
		}

		game.Lock()

		var otherPlayer *Player

		for _, p := range game.players {
			if p == player {
				continue
			}

			otherPlayer = p
		}

		game.Unlock()

		runFSM(game, otherPlayer, player, conn, true)
		return
	}

	player, err := getPlayer(conn)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		return
	}

	player.state = stateSelect

	fmt.Printf("Player connected: %s\n", player.Name)

	game.players[playerName(player.Name)] = &player

	if len(game.players) == 1 {
		player.Left = true
	} else {
		player.Left = false
	}

	err = sendConnectSuccess(conn, player.Left)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		return
	}

	game.Unlock()

	runFSM(game, &player, nil, conn, false)
}

func runFSM(game *GameState, player *Player, otherPlayer *Player, conn net.Conn, remote bool) {
	for {
		switch player.state {
		case stateSelect:
			char, err := getPlayerCharSelect(conn)

			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())

				errMessage := statusMessage{
					Status: err.Error(),
					Code:   ESelect,
				}

				err = sendMessage(conn, errMessage)

				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
				}
			}

			player.Character = char

			err = sendSuccess(conn)

			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
			}

			player.state = stateWaitPlayers
		case stateWaitPlayers:
			fmt.Printf("Waiting other players...\n")

			// get sync message
			content, err := readMessage(conn)

			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
				sendInternalError(conn)
				continue
			}

			fmt.Printf("Got message: %s\n", content)

			game.Lock()

			if len(game.players) < 2 {
				game.Unlock()

				sendNotReady(conn)

				continue
			}

			for _, p := range game.players {
				if p == player {
					continue
				}

				otherPlayer = p
			}

			if otherPlayer.Character == 0 {
				game.Unlock()

				sendNotReady(conn)
				continue
			}

			err = sendSelectedPlayer(conn, otherPlayer)

			if err != nil {
				game.Unlock()

				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
				continue
			}

			game.Unlock()

			player.state = statePlay
		case stateSync: // validate players and then enter the game
			//sendStartPlay(conn
		case statePlay:
			if remote {
				var err error

				select {
				case key := <-otherPlayer.Keys:
					err = sendOtherPlayerKey(conn, key)
				default:
					err = sendOtherPlayerKey(conn, "")
					time.Sleep(time.Millisecond * 500)
				}

				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
					return
				}

			} else {
				key, err := getPlayerKey(conn)

				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
					return
				}

				player.Keys <- key

				fmt.Printf("Player %s pressed key %s\n", player.Name, key)
			}

		default:
			fmt.Printf("Wrong state: %v\n", player.state)
		}
	}
}
