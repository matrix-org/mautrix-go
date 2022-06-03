// Copyright (c) 2022 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bridge

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
	flag "maunium.net/go/mauflag"
	log "maunium.net/go/maulogger/v2"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/appservice/sqlstatestore"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/configupgrade"
	"maunium.net/go/mautrix/util/dbutil"
)

var configPath = flag.MakeFull("c", "config", "The path to your config file.", "config.yaml").String()
var dontSaveConfig = flag.MakeFull("n", "no-update", "Don't save updated config to disk.", "false").Bool()
var registrationPath = flag.MakeFull("r", "registration", "The path where to save the appservice registration.", "registration.yaml").String()
var generateRegistration = flag.MakeFull("g", "generate-registration", "Generate registration and quit.", "false").Bool()
var version = flag.MakeFull("v", "version", "View bridge version and quit.", "false").Bool()
var ignoreUnsupportedDatabase = flag.Make().LongKey("ignore-unsupported-database").Usage("Run even if the database schema is too new").Default("false").Bool()
var ignoreForeignTables = flag.Make().LongKey("ignore-foreign-tables").Usage("Run even if the database contains tables from other programs (like Synapse)").Default("false").Bool()
var wantHelp, _ = flag.MakeHelpFlag()

type Portal interface {
	IsEncrypted() bool
	IsPrivateChat() bool
	MarkEncrypted()
	MainIntent() *appservice.IntentAPI

	ReceiveMatrixEvent(user User, evt *event.Event)
}

type MembershipHandlingPortal interface {
	Portal
	HandleMatrixLeave(sender User)
	HandleMatrixKick(sender User, ghost Ghost)
	HandleMatrixInvite(sender User, ghost Ghost)
}

type ReadReceiptHandlingPortal interface {
	Portal
	HandleMatrixReadReceipt(sender User, eventID id.EventID, receiptTimestamp time.Time)
}

type TypingPortal interface {
	Portal
	HandleMatrixTyping(userIDs []id.UserID)
}

type MetaHandlingPortal interface {
	Portal
	HandleMatrixMeta(sender User, evt *event.Event)
}

type DisappearingPortal interface {
	Portal
	ScheduleDisappearing()
}

type User interface {
	GetPermissionLevel() bridgeconfig.PermissionLevel
	IsLoggedIn() bool
	GetManagementRoomID() id.RoomID
	SetManagementRoom(id.RoomID)
	GetMXID() id.UserID
	GetCommandState() map[string]interface{}
	GetIDoublePuppet() DoublePuppet
	GetIGhost() Ghost
}

type DoublePuppet interface {
	CustomIntent() *appservice.IntentAPI
	SwitchCustomMXID(accessToken string, userID id.UserID) error
}

type Ghost interface {
	DoublePuppet
	DefaultIntent() *appservice.IntentAPI
	GetMXID() id.UserID
}

type ChildOverride interface {
	GetExampleConfig() string
	GetConfigPtr() interface{}

	Init()
	Start()
	Stop()

	GetIPortal(id.RoomID) Portal
	GetIUser(id id.UserID, create bool) User
	IsGhost(id.UserID) bool
	GetIGhost(id.UserID) Ghost
	CreatePrivatePortal(id.RoomID, User, Ghost)
}

type Bridge struct {
	Name         string
	URL          string
	Description  string
	Version      string
	ProtocolName string

	VersionDesc      string
	LinkifiedVersion string
	BuildTime        string

	AS               *appservice.AppService
	EventProcessor   *appservice.EventProcessor
	CommandProcessor CommandProcessor
	MatrixHandler    *MatrixHandler
	Bot              *appservice.IntentAPI
	Config           bridgeconfig.BaseConfig
	ConfigUpgrader   configupgrade.BaseUpgrader
	Log              log.Logger
	DB               *dbutil.Database
	StateStore       *sqlstatestore.SQLStateStore
	Crypto           Crypto
	CryptoPickleKey  string

	Child ChildOverride
}

type Crypto interface {
	HandleMemberEvent(*event.Event)
	Decrypt(*event.Event) (*event.Event, error)
	Encrypt(id.RoomID, event.Type, event.Content) (*event.EncryptedEventContent, error)
	WaitForSession(id.RoomID, id.SenderKey, id.SessionID, time.Duration) bool
	RequestSession(id.RoomID, id.SenderKey, id.SessionID, id.UserID, id.DeviceID)
	ResetSession(id.RoomID)
	Init() error
	Start()
	Stop()
}

func (br *Bridge) GenerateRegistration() {
	if *dontSaveConfig {
		// We need to save the generated as_token and hs_token in the config
		_, _ = fmt.Fprintln(os.Stderr, "--no-update is not compatible with --generate-registration")
		os.Exit(5)
	}
	reg := br.Config.GenerateRegistration()
	err := reg.Save(*registrationPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to save registration:", err)
		os.Exit(21)
	}

	updateTokens := func(helper *configupgrade.Helper) {
		helper.Set(configupgrade.Str, reg.AppToken, "appservice", "as_token")
		helper.Set(configupgrade.Str, reg.ServerToken, "appservice", "hs_token")
	}
	_, _, err = configupgrade.Do(*configPath, true, br.ConfigUpgrader, configupgrade.SimpleUpgrader(updateTokens))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to save config:", err)
		os.Exit(22)
	}
	fmt.Println("Registration generated. See https://docs.mau.fi/bridges/general/registering-appservices.html for instructions on installing the registration.")
	os.Exit(0)
}

func (br *Bridge) InitVersion(tag, commit, buildTime string) {
	if len(tag) > 0 && tag[0] == 'v' {
		tag = tag[1:]
	}
	if tag != br.Version {
		suffix := ""
		if !strings.HasSuffix(br.Version, "+dev") {
			suffix = "+dev"
		}
		if len(commit) > 8 {
			br.Version = fmt.Sprintf("%s%s.%s", br.Version, suffix, commit[:8])
		} else {
			br.Version = fmt.Sprintf("%s%s.unknown", br.Version, suffix)
		}
	}

	br.LinkifiedVersion = fmt.Sprintf("v%s", br.Version)
	if tag == br.Version {
		br.LinkifiedVersion = fmt.Sprintf("[v%s](%s/releases/v%s)", br.Version, br.URL, tag)
	} else if len(commit) > 8 {
		br.LinkifiedVersion = strings.Replace(br.LinkifiedVersion, commit[:8], fmt.Sprintf("[%s](%s/commit/%s)", commit[:8], br.URL, commit), 1)
	}
	mautrix.DefaultUserAgent = fmt.Sprintf("%s/%s %s", br.Name, br.Version, mautrix.DefaultUserAgent)
	br.VersionDesc = fmt.Sprintf("%s %s (%s)", br.Name, br.Version, buildTime)
	br.BuildTime = buildTime
}

func (br *Bridge) ensureConnection() {
	for {
		versions, err := br.Bot.Versions()
		if err != nil {
			br.Log.Errorfln("Failed to connect to homeserver: %v. Retrying in 10 seconds...", err)
			time.Sleep(10 * time.Second)
			continue
		}
		if !versions.ContainsGreaterOrEqual(mautrix.SpecV11) {
			br.Log.Warnfln("Server isn't advertising modern spec versions")
		}
		resp, err := br.Bot.Whoami()
		if err != nil {
			if errors.Is(err, mautrix.MUnknownToken) {
				br.Log.Fatalln("The as_token was not accepted. Is the registration file installed in your homeserver correctly?")
				os.Exit(16)
			} else if errors.Is(err, mautrix.MExclusive) {
				br.Log.Fatalln("The as_token was accepted, but the /register request was not. Are the homeserver domain and username template in the config correct, and do they match the values in the registration?")
				os.Exit(16)
			}
			br.Log.Errorfln("Failed to connect to homeserver: %v. Retrying in 10 seconds...", err)
			time.Sleep(10 * time.Second)
		} else if resp.UserID != br.Bot.UserID {
			br.Log.Fatalln("Unexpected user ID in whoami call: got %s, expected %s", resp.UserID, br.Bot.UserID)
			os.Exit(17)
		} else {
			break
		}
	}
}

func (br *Bridge) UpdateBotProfile() {
	br.Log.Debugln("Updating bot profile")
	botConfig := &br.Config.AppService.Bot

	var err error
	var mxc id.ContentURI
	if botConfig.Avatar == "remove" {
		err = br.Bot.SetAvatarURL(mxc)
	} else if !botConfig.ParsedAvatar.IsEmpty() {
		err = br.Bot.SetAvatarURL(botConfig.ParsedAvatar)
	}
	if err != nil {
		br.Log.Warnln("Failed to update bot avatar:", err)
	}

	if botConfig.Displayname == "remove" {
		err = br.Bot.SetDisplayName("")
	} else if len(botConfig.Displayname) > 0 {
		err = br.Bot.SetDisplayName(botConfig.Displayname)
	}
	if err != nil {
		br.Log.Warnln("Failed to update bot displayname:", err)
	}
}

func (br *Bridge) loadConfig() {
	configData, upgraded, err := configupgrade.Do(*configPath, !*dontSaveConfig, br.ConfigUpgrader)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error updating config:", err)
		if configData == nil {
			os.Exit(10)
		}
	}

	target := br.Child.GetConfigPtr()
	if !upgraded {
		// Fallback: if config upgrading failed, load example config for base values
		err = yaml.Unmarshal([]byte(br.Child.GetExampleConfig()), &target)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Failed to unmarshal example config:", err)
			os.Exit(10)
		}
	}
	err = yaml.Unmarshal(configData, target)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to parse config:", err)
		os.Exit(10)
	}
}

func (br *Bridge) init() {
	var err error

	br.AS = br.Config.MakeAppService()
	_, _ = br.AS.Init()

	br.Log = log.Create()
	br.Config.Logging.Configure(br.Log)
	log.DefaultLogger = br.Log.(*log.BasicLogger)
	if len(br.Config.Logging.FileNameFormat) > 0 {
		err = log.OpenFile()
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Failed to open log file:", err)
			os.Exit(12)
		}
	}
	br.AS.Log = log.Sub("Matrix")
	br.Bot = br.AS.BotIntent()
	br.Log.Infoln("Initializing", br.VersionDesc)

	br.Log.Debugln("Initializing database connection")
	br.DB, err = dbutil.NewFromConfig(br.Name, br.Config.AppService.Database, br.Log.Sub("Database"))
	if err != nil {
		br.Log.Fatalln("Failed to initialize database connection:", err)
		os.Exit(14)
	}
	br.DB.IgnoreUnsupportedDatabase = *ignoreUnsupportedDatabase
	br.DB.IgnoreForeignTables = *ignoreForeignTables

	br.Log.Debugln("Initializing state store")
	br.StateStore = sqlstatestore.NewSQLStateStore(br.DB)
	br.AS.StateStore = br.StateStore

	br.Log.Debugln("Initializing Matrix event processor")
	br.EventProcessor = appservice.NewEventProcessor(br.AS)
	br.EventProcessor.ExecMode = appservice.Sync
	br.Log.Debugln("Initializing Matrix event handler")
	br.MatrixHandler = NewMatrixHandler(br)

	br.Crypto = NewCryptoHelper(br)

	br.Child.Init()
}

func (br *Bridge) LogDBUpgradeErrorAndExit(name string, err error) {
	br.Log.Fatalfln("Failed to initialize %s: %v", name, err)
	if errors.Is(err, dbutil.ErrForeignTables) {
		br.Log.Infoln("You can use --ignore-foreign-tables to ignore this error")
	} else if errors.Is(err, dbutil.ErrNotOwned) {
		br.Log.Infoln("Sharing the same database with different programs is not supported")
	} else if errors.Is(err, dbutil.ErrUnsupportedDatabaseVersion) {
		br.Log.Infoln("Downgrading the bridge is not supported")
	}
	os.Exit(15)
}

func (br *Bridge) start() {
	br.Log.Debugln("Running database upgrades")
	err := br.DB.Upgrade()
	if err != nil {
		br.LogDBUpgradeErrorAndExit("main database", err)
	} else if err = br.StateStore.Upgrade(); err != nil {
		br.LogDBUpgradeErrorAndExit("matrix state store", err)
	}

	br.Log.Debugln("Checking connection to homeserver")
	br.ensureConnection()

	if br.Crypto != nil {
		err = br.Crypto.Init()
		if err != nil {
			br.Log.Fatalln("Error initializing end-to-bridge encryption:", err)
			os.Exit(19)
		}
	}

	br.Log.Debugln("Starting application service HTTP server")
	go br.AS.Start()
	br.Log.Debugln("Starting event processor")
	go br.EventProcessor.Start()

	go br.UpdateBotProfile()
	if br.Crypto != nil {
		go br.Crypto.Start()
	}

	br.Child.Start()
	br.AS.Ready = true
}

func (br *Bridge) stop() {
	if br.Crypto != nil {
		br.Crypto.Stop()
	}
	br.AS.Stop()
	br.EventProcessor.Stop()
	br.Child.Stop()
}

func (br *Bridge) Main() {
	flag.SetHelpTitles(
		fmt.Sprintf("%s - %s", br.Name, br.Description),
		fmt.Sprintf("%s [-hgvn] [-c <path>] [-r <path>]", br.Name))
	err := flag.Parse()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		flag.PrintHelp()
		os.Exit(1)
	} else if *wantHelp {
		flag.PrintHelp()
		os.Exit(0)
	} else if *version {
		fmt.Println(br.VersionDesc)
		return
	}

	br.loadConfig()

	if *generateRegistration {
		br.GenerateRegistration()
		return
	}

	br.init()
	br.Log.Infoln("Bridge initialization complete, starting...")
	br.start()
	br.Log.Infoln("Bridge started!")

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	br.Log.Infoln("Interrupt received, stopping...")
	br.stop()
	br.Log.Infoln("Bridge stopped.")
	os.Exit(0)
}
