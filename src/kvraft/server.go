package raftkv

import (
	"encoding/gob"
	"labrpc"
	"log"
	"raft"
	"sync"
	"time"
)

const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}

const (
	OP_PUT         = "Put"
	OP_APPEND      = "Append"
	OP_GET         = "Get"
	CLIENT_TIMEOUT = 1000 * time.Millisecond
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.

	Kind      string
	Key       string
	Value     string
	SessionID int64
	ReqID     int
}

type RaftKV struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.

	data    map[string]string
	results map[int]chan Op
	history map[int64]int
}

func (kv *RaftKV) CheckDuplicate(entry Op) bool {
	id, ok := kv.history[entry.SessionID]
	if ok {
		return id >= entry.ReqID
	}
	return false
}

func (kv *RaftKV) AppendLogEntry(entry Op) bool {
	index, _, isLeader := kv.rf.Start(entry)

	if !isLeader {
		return false
	}

	kv.mu.Lock()
	ch, ok := kv.results[index]
	if !ok {
		ch = make(chan Op, 1)
		kv.results[index] = ch
	}
	kv.mu.Unlock()

	select {
	case op := <-ch:
		return op == entry
	case <-time.After(CLIENT_TIMEOUT):
		return false
	}
}

func (kv *RaftKV) Apply(op Op) {
	switch op.Kind {
	case OP_PUT:
		kv.data[op.Key] = op.Value
	case OP_APPEND:
		kv.data[op.Key] += op.Value
	}
	kv.history[op.SessionID] = op.ReqID
}

func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	entry := Op{Kind: OP_GET, Key: args.Key, SessionID: args.SessionID, ReqID: args.ReqID}

	ok := kv.AppendLogEntry(entry)
	if !ok {
		reply.WrongLeader = true
		return
	}

	reply.Err = OK
	reply.WrongLeader = false
	kv.mu.Lock()
	defer kv.mu.Unlock()

	reply.Value = kv.data[args.Key]
	kv.history[args.SessionID] = args.ReqID

}

func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	entry := Op{Kind: args.Op, Key: args.Key, Value: args.Value, SessionID: args.SessionID, ReqID: args.ReqID}

	ok := kv.AppendLogEntry(entry)

	if !ok {
		reply.WrongLeader = true
		return
	}

	reply.WrongLeader = false
	reply.Err = OK

}

//
// the tester calls Kill() when a RaftKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *RaftKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *RaftKV) Init() {
	kv.data = make(map[string]string)
	kv.results = make(map[int]chan Op)
	kv.history = make(map[int64]int)
	kv.applyCh = make(chan raft.ApplyMsg, 100)
}

func (kv *RaftKV) Loop() {
	for {
		msg := <-kv.applyCh
		op := msg.Command.(Op)

		kv.mu.Lock()

		if !kv.CheckDuplicate(op) {
			kv.Apply(op)
		}

		ch, ok := kv.results[msg.Index]

		if ok {
			// clear the chan for the index
			select {
			case <-ch:
			default:
			}
			ch <- op
		} else {
			// when the kv is a follower
			kv.results[msg.Index] = make(chan Op,1)
		}

		kv.mu.Unlock()
	}
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots with persister.SaveSnapshot(),
// and Raft should save its state (including log) with persister.SaveRaftState().
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *RaftKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(RaftKV)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.

	kv.Init()
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	go kv.Loop()
	// You may need initialization code here.

	return kv
}
