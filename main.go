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
		Character string `json:"character"`
		player    string `json:"player"`
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
)

func (c Character) String() string {
	if c == Max {
		return "Max"
	}

	if c == Drax {
		return "Drax"
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

func sendMessage(conn net.Conn, message statusMessage) error {
	data, err := json.Marshal(&message)

	if err != nil {
		return err
	}

	_, err = conn.Write(data)

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

func getPlayer(conn net.Conn) (Player, error) {
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)

	if err != nil {
		return Player{}, err
	}

	fmt.Printf("Received: %s\n", string(buf[:n]))

	var player Player
	err = json.Unmarshal(buf[:n], &player)

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

func handleRequest(conn net.Conn, game *GameState) {
	fmt.Printf("Connection established from %s\n", conn.RemoteAddr())

	defer conn.Close()

	game.Lock()

	if len(game.players) >= 2 {
		game.Unlock()
		toManyPlayers(conn)
		return
	}

	/*err := connectionSuccess(conn)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s", err.Error())
		return
	}*/

	player, err := getPlayer(conn)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s", err.Error())
		return
	}

	player.state = stateSelect

	fmt.Printf("Player connected: %s\n", player.Name)

	game.players[playerName(player.Name)] = &player

	game.Unlock()

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
			time.Sleep(3 * time.Second)
		case stateSync: // validate players and then enter the game
		case statePlay:
			// todo
		default:
			fmt.Printf("Wrong state: %v\n", player.state)
		}
	}
}
