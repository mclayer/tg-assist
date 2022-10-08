package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"github.com/zelenin/go-tdlib/client"
	"gopkg.in/yaml.v3"
)

type ReplaceWords struct {
	From string
	To string
}
type Chat struct {
	Id int64
	Name string
	ReplaceWords []ReplaceWords
}
type Config struct {
	ApiId int32
	ApiHash string
	KeywordToDelete string
	MessageToPost string
	Chats []Chat

}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

var es *elasticsearch.Client
var iClient *client.Client
var config Config

func main() {
	authorizer := client.ClientAuthorizer()
	go client.CliInteractor(authorizer)


	applyConfig()

	authorizer.TdlibParameters <- &client.TdlibParameters{
		UseTestDc:              false,
		DatabaseDirectory:      filepath.Join(".tdlib", "database"),
		FilesDirectory:         filepath.Join(".tdlib", "files"),
		UseFileDatabase:        true,
		UseChatInfoDatabase:    true,
		UseMessageDatabase:     true,
		UseSecretChats:         false,
		ApiId:                  config.ApiId,
		ApiHash:                config.ApiHash,
		SystemLanguageCode:     "en",
		DeviceModel:            "Server",
		SystemVersion:          "1.0.0",
		ApplicationVersion:     "1.0.0",
		EnableStorageOptimizer: false,
		IgnoreFileNames:        false,
	}
	logVerbosity := client.WithLogVerbosity(&client.SetLogVerbosityLevelRequest{
		NewVerbosityLevel: 2,
	})
	var err error
	iClient, err = client.NewClient(authorizer, logVerbosity)
	if err != nil {
		log.Fatalf("NewClient error: %s", err)
	}

	optionValue, err := iClient.GetOption(&client.GetOptionRequest{
		Name: "version",
	})
	if err != nil {
		log.Fatalf("GetOption error: %s", err)
	}

	log.Printf("TDLib version: %s", optionValue.(*client.OptionValueString).Value)

	me := getMe()
	log.Printf("Me: %s %s [%s]", me.FirstName, me.LastName, me.Username)


	es, _ := elasticsearch.NewDefaultClient()
	log.Println(elasticsearch.Version)
	log.Println(es.Info())

	// getChats()
	// cleanMyOldMessages()
	// showLastGroupMessages()
	// getFolders()
	// getChatsFromFolder()
	// Autopost()
	logUpdates()
}
func applyConfig() {
	file, err := ioutil.ReadFile("config.local.yml")
	check(err)

	err = yaml.Unmarshal([]byte(file), &config)
	check(err)
}
func getChatsFromFolder() []int64 {
	folder, err := iClient.GetChatFilter(&client.GetChatFilterRequest{
		ChatFilterId: 6,
	})
	if err != nil {
		log.Fatalf("GetChatFilter error: %s", err)
	}
	return folder.IncludedChatIds
}

func getFolders() {
	listener := iClient.GetListener()
	defer listener.Close()

	for update := range listener.Updates {
		if update.GetClass() == client.ClassUpdate {
			log.Printf("%#v", update)
			if update.GetType() == client.TypeUpdateChatFilters {
				for chat := range update.(*client.UpdateChatFilters).ChatFilters{
					log.Printf("%#v", chat)
				}
				return
			}
		} else {
			log.Printf("NON: %#v", update)
		}
	}
}



func showLastGroupMessages() {
	for _, chat := range getMyChats() {
		chatTypeSupergroup, ok := chat.Type.(*client.ChatTypeSupergroup)
		if !ok {
			continue
		}
		group, err := iClient.GetSupergroup(&client.GetSupergroupRequest{
			SupergroupId: chatTypeSupergroup.SupergroupId,
		})
		if err != nil {
			log.Fatalf("GetSupergroup error: %s", err)
		}
		fmt.Println("#####################################################")
		fmt.Printf("@%s\n", group	.Username)
		msgs, err := iClient.GetChatHistory(&client.GetChatHistoryRequest{
			ChatId: chat.Id,
			FromMessageId: 0,
			Limit: 5,
			OnlyLocal: false,
			Offset: 0,
		})
		if err != nil {
			log.Fatalf("GetChatHistory error: %s", err)
		}
		fmt.Printf("msgs: %d\n", msgs.TotalCount)
		for i, msg := range msgs.Messages {
			fmt.Printf("msgType: %s\n", msg.Content.MessageContentType())
			if msg.Content.MessageContentType() != "messageText" {
				continue
			}
			fmt.Printf("msg.sender: %v\n", msg.SenderId)

			if msg.SenderId == nil || msg.SenderId.MessageSenderType() != "MessageSenderUser" {
				continue
			}

			sender, err := iClient.GetUser(&client.GetUserRequest{
				UserId: msg.SenderId.(*client.MessageSenderUser).UserId,
			})
			if err != nil {
				log.Fatalf("GetUser error: %s", err)
			}

			fmt.Printf("%d:\t%s\t%s\t(@%s)\t%s\n", i, sender.FirstName, sender.LastName, sender.Username , msg.Content.(*client.MessageText).Text.Text)
		}
		fmt.Println("----------------------------------------------")

	}
}
func getMe() (*client.User) {
	me, err := iClient.GetMe()
	if err != nil {
		log.Fatalf("GetMe error: %s", err)
	}
	return me
}
type ChatInfo struct {
	chatId int64
	group *client.Supergroup
}

func getChatsForAutoPost() []ChatInfo {
	chatsFromFolder := getChatsFromFolder()
	chats := make([]ChatInfo, 0, len(chatsFromFolder))
	for _, chatId := range chatsFromFolder {
		chat, err := iClient.GetChat(&client.GetChatRequest{
			ChatId: chatId,
		})
		if err != nil {
			log.Fatalf("getChat error: %s", err)
		}
		log.Printf("%s\n", chat.Title)
		chatTypeSupergroup, ok := chat.Type.(*client.ChatTypeSupergroup)
		if !ok {
			continue
		}
		group, err := iClient.GetSupergroup(&client.GetSupergroupRequest{
			SupergroupId: chatTypeSupergroup.SupergroupId,
		})
		if err != nil {
			log.Fatalf("GetSupergroup error: %s", err)
		}
		chats = append(chats, ChatInfo{
			chatId: chatId,
			group: group,
		})
	}
	return chats
}

func Autopost() {
	me := getMe()
	myId := me.Id
	fmt.Printf("%d", myId)
	for _, chat := range getChatsForAutoPost() {
		fmt.Printf("checking %s\n", chat.group.Username)
		msgs, err := iClient.SearchChatMessages(&client.SearchChatMessagesRequest{
			ChatId: chat.chatId,
			Query: config.KeywordToDelete,
			SenderId: &client.MessageSenderUser{
				UserId: myId,
			},
			FromMessageId: 0,
			Offset: 0,
			Limit: 2,
			Filter: &client.SearchMessagesFilterEmpty{},
		})
		if err != nil {
			log.Fatalf("SearchChatMessages error: %s", err)
		}
		var lastMsgs *client.Messages
		log.Print("Getting last msgs...")
		for i := 0; i < 20; i++ {
			lastMsgs, _ = iClient.GetChatHistory(&client.GetChatHistoryRequest{
				ChatId: chat.chatId,
				FromMessageId: 0,
				Offset: 0,
				Limit: 20,
				OnlyLocal: false,
			})
			if(lastMsgs.TotalCount >= 20) {
				continue
			}
			log.Printf("got only %d. trying again...", lastMsgs.TotalCount)
			lastMsgs = nil
			time.Sleep(1 * time.Second)
		}
		if lastMsgs == nil {
			log.Fatalf("Can't get all last msgs")
		}
		log.Println("OK")

		now := time.Now().Unix()
		var deleted int32 = 0

		if msgs.TotalCount >= 1 {
			outer:
			for i, msg := range msgs.Messages{
				fmt.Printf("@%s:\t%d:\t%s\t%d\t%s...",  chat.group.Username,  i, msg.SenderId, msg.Date, msg.Content)

				if now - int64(msg.Date) < 3600 {
					continue
				}

				for _, lastMsg := range lastMsgs.Messages {
					if lastMsg.Id == msg.Id {
						continue outer;
					}
				}

				if !msg.CanBeDeletedForAllUsers {
					log.Fatal("CanBeDeletedForAllUsers = false")
				}

				ok, err := iClient.DeleteMessages(&client.DeleteMessagesRequest{
					ChatId: chat.chatId,
					MessageIds: []int64{msg.Id},
					Revoke: true,
				})

				if ok == nil || err != nil {
					log.Fatalf("Delesa teMessages error: %s", err)
				}
				fmt.Println("deleted")
				deleted++
			}
		}

		if msgs.TotalCount - deleted == 0{
			fmt.Printf("sending to %s\n", chat.group.Username)
			sendMessage(chat.chatId)
			time.Sleep(time.Duration(8 + rand.Intn(90)) * time.Second)
  		//os.Exit(1)
		} else {
			println("ignoring")
		}
	}
}

func getMyChats() ([]*client.Chat) {
	var chatList client.ChatList
	var req client.GetChatsRequest
	req.Limit = 400
	req.ChatList = chatList
	chats, err := iClient.GetChats(&req)
	if err != nil {
		log.Fatalf("GetChats error: %s", err)
	}
	var aChats []*client.Chat

	for _, id := range chats.ChatIds {
		var chatReq client.GetChatRequest
		chatReq.ChatId = id
		chat, err := iClient.GetChat(&chatReq)
		if err != nil {
			log.Fatalf("GetChat error: %s", err)
		}
		aChats = append(aChats, chat)
	}
	return aChats
}

func printGroups() {

	for _, chat := range getMyChats() {
		if chat.Type.ChatTypeType() == "chatTypePrivate" {
			continue
		}

		chatTypeSupergroup, ok := chat.Type.(*client.ChatTypeSupergroup)
		if ok {
			supergroup, err := iClient.GetSupergroup(&client.GetSupergroupRequest{
				SupergroupId: chatTypeSupergroup.SupergroupId,
			})
			if err != nil {
				log.Fatalf("GetSupergroup error: %s", err)
			}
			fmt.Printf("@%s\t", supergroup.Username)
			// fmt.Printf("%d\t", supergroup.MemberCount)
		}
		fmt.Printf("%s\t", chat.Title)
		fmt.Println("")
	}
	// bytes, err := yaml.Marshal(chats)
	// if err != nil {
	// 	log.Fatalf("yaml.Marshal error: %s", err)
	// }
	// fmt.Println(string(bytes))
}


func sendMessage(chatId int64) {

	text:= getMessage(chatId)

	_, err := iClient.SendMessage(&client.SendMessageRequest{
		ChatId: chatId,
		InputMessageContent: &client.InputMessageText{
			Text: &client.FormattedText{
						Text: text,
			},
			DisableWebPagePreview: true,
			ClearDraft: true,
		},
	})
	if err != nil {
		log.Println(err.Error()[0:3])
		if err.Error()[0:3] == "400" {
			log.Println(err.Error())
		} else {
			log.Fatalf("sendMessage error: %s", err)
		}
	}
}


func getMessage(chatId int64) string {
	text:= config.MessageToPost
	for _, chat := range(config.Chats) {
		if (chatId != chat.Id) {
			continue
		}
		for _, replaceWords := range(chat.ReplaceWords) {
			text = strings.Replace(text, replaceWords.From, replaceWords.To, -1)
		}
		break
	}
	return text
}
func logUpdates() {

	listener := iClient.GetListener()
	defer listener.Close()

	for update := range listener.Updates {
			log.Printf("%v %v %#v",update.GetClass(), update.GetType(), update)
			saveUpdate(update)
	}
}

func convertUserStatus(userStatus client.UserStatus) string {
	return strings.ToLower(strings.Replace(userStatus.UserStatusType(), "userStatus", "", 1))
}
func getLastOnlineTimestamp(update *client.UpdateUserStatus) int64  {
	var lastOnlineTime time.Time
	switch update.Status.UserStatusType() {
	case client.TypeUserStatusLastMonth:
		lastOnlineTime = time.Now().Add(time.Duration(-2 * 30 * 24 * time.Hour))
	case client.TypeUserStatusLastWeek:
		lastOnlineTime = time.Now().Add(time.Duration(-2 * -7 * 24 * time.Hour))
	case client.TypeUserStatusRecently:
		lastOnlineTime = time.Now().Add(time.Duration(-7 * 24 * time.Hour))
	case client.TypeUserStatusOffline:
		typedStatus := update.Status.(*client.UserStatusOffline)
		lastOnlineTime = time.Unix(int64(typedStatus.WasOnline), 0)
	case client.TypeUserStatusOnline:
		typedStatus := update.Status.(*client.UserStatusOnline)
		lastOnlineTime = time.Unix(int64(typedStatus.Expires), 0)
	}
	return lastOnlineTime.Unix()
}

func saveUpdate(update client.Type) {
	type UpdateUserStatus struct {
		time time.Time
		userId int64
		status string
		lastOnline int64
	}

	var doc interface{}
		if updateUserStatus, ok := update.(*client.UpdateUserStatus); ok {
			doc = UpdateUserStatus{
				time: time.Now(),
				userId: updateUserStatus.UserId,
				status: convertUserStatus(updateUserStatus.Status),
				lastOnline: getLastOnlineTimestamp(updateUserStatus),
			}
		} else {
			return
		}


	res, _ := es.Index("tg-assist.updates.user-status", esutil.NewJSONReader(&doc))
	fmt.Println(res)
}
// func saveUpdate(update client.Type) {

// 	type UpdateUserStatus struct {
// 		time int64
// 		UserId int64
// 		NewStatus string
// 	}

// 	nowNano := time.Now().UnixNano()
// 	updateUserStatus, ok := update.(*client.UpdateUserStatus)
// 	if ok {
// 		updateUserStatus
// 	}
// 	if update.getClass() == client.UpdateUserStatus {
// 		UserId int64
// 		NewStatus string
// 	}
// 	UpdateUserStatus


// 	writer := parquet.NewGenericWriter[RowType](output)

// 	_, err := writer.Write([]RowType{
// 			...
// 	})
// 	if err != nil {
// 			...
// 	}

// 	// Closing the writer is necessary to flush buffers and write the file footer.
// 	if err := writer.Close(); err != nil {
// 			...
// 	}


// 	import (
// 		"github.com/apache/arrow/go/v9/arrow/array"
// 		"github.com/apache/arrow/go/v9/arrow/math"
// 		"github.com/apache/arrow/go/v9/arrow/memory"
// 	)

// 		fb := array.NewFloat64Builder(memory.DefaultAllocator)

// 		fb.AppendValues([]float64{1, 3, 5, 7, 9, 11}, nil)

// 		vec := fb.NewFloat64Array()
// 		math.Float64.Sum(vec)


// 	///start


// 	func main() {
// 		fw, err := local.NewLocalFileWriter("arrow.parquet")
// 		if err != nil {
// 			log.Println("Can't create file", err)
// 			return
// 		}
// 		mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
// 		schema := arrow.NewSchema(
// 			[]arrow.Field{
// 				{Name: "int64", Type: arrow.PrimitiveTypes.Int64},
// 				{Name: "float64", Type: arrow.PrimitiveTypes.Float64},
// 				{Name: "str", Type: arrow.BinaryTypes.String},
// 				{Name: "ts_ms", Type: arrow.FixedWidthTypes.Timestamp_ms},
// 				{Name: "nullable-int32", Type: arrow.PrimitiveTypes.Int32,
// 					Nullable: true},
// 			},
// 			nil,
// 		)
// 		b := array.NewRecordBuilder(mem, schema)
// 		defer b.Release()
// 		for idx := range schema.Fields() {
// 			switch idx {
// 			case 0:
// 				b.Field(idx).(*array.Int64Builder).AppendValues(
// 					[]int64{int64(1), int64(2), int64(3)}, nil,
// 				)
// 			case 1:
// 				b.Field(idx).(*array.Float64Builder).AppendValues(
// 					[]float64{float64(1.1), float64(1.2), float64(1.3)}, nil,
// 				)
// 			case 2:
// 				b.Field(idx).(*array.StringBuilder).AppendValues(
// 					[]string{"a", "b", "c"}, nil,
// 				)
// 			case 3:
// 				n := arrow.Timestamp(time.Now().UnixMilli())
// 				b.Field(idx).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{n, n, n}, nil)
// 			case 4:
// 				colBuilder := b.Field(idx).(*array.Int32Builder)
// 				colBuilder.Append(1)
// 				colBuilder.AppendNull()
// 				colBuilder.Append(2)
// 				colBuilder.AppendNull()
// 			}
// 		}
// 		rec := b.NewRecord()

// 		w, err := writer.NewArrowWriter(schema, fw, 1)
// 		if err != nil {
// 			log.Println("Can't create parquet writer", err)
// 			return
// 		}
// 		if err = w.WriteArrow(rec); err != nil {
// 			log.Println("WriteArrow error", err)
// 			return
// 		}
// 		if err = w.WriteStop(); err != nil {
// 			log.Println("WriteStop error", err)
// 			return
// 		}
// 		log.Println("Write Finished")
// 		fw.Close()

// 		fr, err := local.NewLocalFileReader("arrow.parquet")
// 		if err != nil {
// 			log.Println("Can't open file for read", err)
// 			return
// 		}

// 		pr, err := reader.NewParquetReader(fr, nil, 1)
// 		if err != nil {
// 			log.Println("Can't create parquet reader", err)
// 			return
// 		}

// 		num := int(pr.GetNumRows())
// 		res, err := pr.ReadByNumber(num)
// 		if err != nil {
// 			log.Println("Can't read rows", err)
// 			return
// 		}

// 		table := ""
// 		for _, row := range res {
// 			table = table + fmt.Sprintf("%v\n", row)
// 		}

// 		log.Printf("Content of table:\n%s", table)
// 		log.Print("Read Finished")
// 	}
// 	///emd
// }
