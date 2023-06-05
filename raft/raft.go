package raft

//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"

	"bytes"
	"log"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"6.824/labgob"
	"6.824/labrpc"
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

//
// Server must be in one of the three states:
// Leader, Candidate, Follower
//
const (
	Leader                      = 1532675
	Candidate                   = 4884264
	Follower                    = 8423584
	ElectionInterval            = 450
	HeartBeatsInterval          = 150
	RetryReplicateEntryInterval = 100
)

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	state       int // Leader, Candidate, Follower
	lastReceive time.Time

	// Persistent state on all servers
	currentTerm int
	votedFor    int
	log         []Entry

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	// Volatile state on leaders
	nextIndex  []int
	matchIndex []int

	applyCh chan ApplyMsg
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	// Your code here (3A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isleader = rf.state == Leader
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var logs []Entry
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&logs) != nil {
		log.Printf("Decode error!")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = logs
	}
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}

//
// example RequestVote RPC handler. Receiver implementation.
// Invoked by candidates to gather votes (§5.2).
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// log.Printf("%v receive RV RPC from %v", rf.me, args.CandidateId)
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm

	// All Servers: If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (§5.1)
	if args.Term > rf.currentTerm {
		rf.ConvertToFollower(args.Term)
	}

	if rf.state == Follower {
		// 1. Reply false if term < currentTerm (§5.1)
		if args.Term < rf.currentTerm {
			reply.VoteGranted = false
			return
		}
		// 2. If votedFor is null or candidateId, and candidate’s log is at
		// least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)
		lastLogIndex, lastLogTerm := rf.getLastLog()
		if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
			if args.LastLogTerm > lastLogTerm ||
				(args.LastLogTerm == lastLogTerm &&
					args.LastLogIndex >= lastLogIndex) {
				reply.VoteGranted = true
				rf.votedFor = args.CandidateId
				rf.persist()
				// log.Printf("%d voted %d for term %d", rf.me, args.CandidateId, args.Term)
				rf.lastReceive = time.Now()
				return
			}
		}
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

//
// example AppendEntries RPC arguments structure.
// field names must start with capital letters!
// my invariate: Entries index must be continuous increasing.
//
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Entry
	LeaderCommit int
}

//
// example AppendEntries RPC reply structure.
// field names must start with capital letters!
//
type AppendEntriesReply struct {
	Term    int
	Success bool
	XTerm   int
	XIndex  int
	XLen    int
}

//
// example AppendEntries RPC handler. Receiver implementation.
// Invoked by leader to replicate log entries (§5.3); also used as
// heartbeat (§5.2).
//
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// log.Printf("%v receive AE RPC from %v", rf.me, args.LeaderId)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm

	// All Servers: If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (§5.1)
	if args.Term > rf.currentTerm {
		rf.ConvertToFollower(args.Term)
	}

	// 1. Reply false if term < currentTerm (§5.1)
	if args.Term < rf.currentTerm {
		reply.Success = false
		// log.Printf("%v AE from %v fail at 1", rf.me, args.LeaderId)
		return
	}

	if rf.state == Candidate {
		rf.lastReceive = time.Now()
		rf.ConvertToFollower(args.Term)
	} else if rf.state == Follower {
		rf.lastReceive = time.Now()
	}

	// 2. Reply false if log doesn’t contain an entry at prevLogIndex
	// whose term matches prevLogTerm (§5.3)
	if args.PrevLogIndex >= len(rf.log) {
		reply.Success = false
		reply.XTerm = -1
		reply.XIndex = -1
		reply.XLen = len(rf.log)
		return
	}

	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		reply.XTerm = rf.log[args.PrevLogIndex].Term
		reply.XIndex = rf.log[args.PrevLogIndex].Index
		for i := args.PrevLogIndex; i >= 1 && rf.log[i].Term == rf.log[args.PrevLogIndex].Term; i-- {
			reply.XIndex = rf.log[i].Index
		}
		return
	}

	// 3. If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it. (§5.3)
	deleteFromIdx := len(rf.log)
	for _, entry := range args.Entries {
		if entry.Index < len(rf.log) && entry.Term != rf.log[entry.Index].Term {
			// deleteFromIdx = int(math.Min(float64(deleteFromIdx), float64(entry.Index)))
			deleteFromIdx = entry.Index
			break
		}
	}
	rf.log = rf.log[:deleteFromIdx]

	// 4. Append any new entries not already in the log
	for idx, entry := range args.Entries {
		if entry.Index == deleteFromIdx {
			for i := idx; i < len(args.Entries); i++ {
				rf.log = append(rf.log, args.Entries[i])
			}
			// rf.log = append(rf.log, args.Entries[idx:]...)
			rf.persist()
			// log.Printf("%v append from %v, new log length %v", rf.me, idx, len(rf.log))
			break
		}
	}

	// 5. If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = int(math.Min(float64(args.LeaderCommit), float64(len(rf.log)-1)))
		// All Servers: If commitIndex > lastApplied: increment lastApplied, apply
		// log[lastApplied] to state machine (§5.3)
		rf.applyLog()
	}

	reply.Success = true
}

//
// All Servers: If commitIndex > lastApplied: increment lastApplied, apply
// log[lastApplied] to state machine (§5.3)
//
func (rf *Raft) applyLog() {
	for rf.commitIndex > rf.lastApplied {
		rf.lastApplied++
		rf.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      rf.log[rf.lastApplied].Command,
			CommandIndex: rf.log[rf.lastApplied].Index,
		}
	}
}

//
// example code to send an AppendEntries RPC.
//
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

//
// Leader server send heartbeats periodically.
//
func (rf *Raft) sendHeartBeatsPeriodically() {
	_, isLeader := rf.GetState()

	for !rf.killed() && isLeader {
		rf.mu.Lock()
		lastLogIndex, lastLogTerm := rf.getLastLog()
		term := rf.currentTerm
		leaderId := rf.me
		leaderCommit := rf.commitIndex
		rf.mu.Unlock()

		for i := 0; i < len(rf.peers); i++ {
			if rf.killed() {
				return
			}
			if i == rf.me {
				continue
			}
			go func(peer int) {
				args := AppendEntriesArgs{
					Term:         term,
					LeaderId:     leaderId,
					PrevLogIndex: lastLogIndex,
					PrevLogTerm:  lastLogTerm,
					Entries:      []Entry{},
					LeaderCommit: leaderCommit,
				}
				reply := AppendEntriesReply{}
				ok := rf.sendAppendEntries(peer, &args, &reply)
				if !ok {
					return
				}
				// All Servers: If RPC request or response contains term T > currentTerm:
				// set currentTerm = T, convert to follower (§5.1)
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Term > rf.currentTerm {
					rf.ConvertToFollower(reply.Term)
				}
			}(i)
		}

		_, isLeader = rf.GetState()

		time.Sleep(time.Duration(HeartBeatsInterval) * time.Millisecond)
	}
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	// Your code here (3B).
	term, isLeader := rf.GetState()

	// if this server isn't the leader, returns false.
	if rf.killed() || !isLeader {
		return -1, -1, false
	}

	// otherwise start the agreement and return immediately.
	rf.mu.Lock()
	defer rf.mu.Unlock()
	index := len(rf.log)
	entry := Entry{Term: rf.currentTerm, Command: command, Index: index}
	// If command received from client: append entry to local log,
	// respond after entry applied to state machine (§5.3)
	rf.log = append(rf.log, entry)
	rf.persist()
	go rf.startAgreement()
	// the first return value is the index that the command will appear at
	// if it's ever committed. the second return value is the current
	// term. the third return value is true if this server believes it is
	// the leader.
	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for !rf.killed() {
		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		electionCountDown := ElectionInterval + rand.Intn(150)
		startTime := time.Now()
		time.Sleep(time.Duration(electionCountDown) * time.Millisecond)

		func() {
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if rf.lastReceive.Before(startTime) {
				if rf.state != Leader {
					go rf.InitiateElection()
				}
			}
		}()
	}
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (3A, 3B, 3C).
	rf.state = Follower
	rf.lastReceive = time.Now()

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]Entry, 0)
	rf.log = append(rf.log, Entry{Term: 0, Index: 0})

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.applyCh = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}

//
// Change the server state to Leader.
// Only apply when candidate receives votes from majority of servers.
//
func (rf *Raft) ConvertToLeader() {
	rf.state = Leader

	n_svr := len(rf.peers)
	n_log := len(rf.log)
	rf.nextIndex = make([]int, n_svr)
	rf.matchIndex = make([]int, n_svr)
	for i := 0; i < n_svr; i++ {
		rf.nextIndex[i] = n_log
		rf.matchIndex[i] = 0
	}

	// Leaders: Upon election: send initial empty AppendEntries RPCs
	// (heartbeat) to each server; repeat during idle periods to
	// prevent election timeouts (§5.2)
	go rf.sendHeartBeatsPeriodically()
}

//
// Change the server state to Candidate.
// Only apply when one of the followings happens:
// (1) Follower times out, starts election.
// (2) Candidate times out, start election.
//
func (rf *Raft) ConvertToCandidate() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persist()
}

//
// Change the server state to Follower.
// Only apply when one of the followings happens:
// (1) Candidate discovers current leader or new term.
// (2) Leader discovers server with higher term.
//
func (rf *Raft) ConvertToFollower(term int) {
	rf.state = Follower
	rf.votedFor = -1
	rf.currentTerm = term
	rf.persist()
}

//
// Let the server initiate an election.
//
func (rf *Raft) InitiateElection() {
	rf.mu.Lock()
	rf.lastReceive = time.Now()
	rf.ConvertToCandidate()
	term := rf.currentTerm
	lastLogIndex, lastLogTerm := rf.getLastLog()
	args := RequestVoteArgs{
		Term:         term,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	rf.mu.Unlock()

	voteCount := 1
	voteFinish := 1
	var mu sync.Mutex
	cond := sync.NewCond(&mu)

	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		go func(peer int) {
			reply := RequestVoteReply{}
			ok := rf.sendRequestVote(peer, &args, &reply)
			if !ok {
				return
			}
			// All Servers: If RPC request or response contains term T > currentTerm:
			// set currentTerm = T, convert to follower (§5.1)
			// TO BE DEBUGGED: currentTerm may differ from term!
			if reply.Term > term {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				rf.ConvertToFollower(reply.Term)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if reply.VoteGranted {
				voteCount++
			}
			voteFinish++
			cond.Broadcast()
		}(i)
	}

	mu.Lock()
	defer mu.Unlock()
	for voteCount < len(rf.peers)/2+1 && voteFinish != len(rf.peers) {
		cond.Wait()
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Candidate {
		// log.Printf("%d quits the election since it's no more candidate of current term", rf.me)
		return
	}
	if rf.currentTerm > term {
		rf.ConvertToFollower(rf.currentTerm)
		return
	}
	if voteCount >= len(rf.peers)/2+1 {
		// log.Printf("%d wins the election!", rf.me)
		rf.ConvertToLeader()
	} else {
		// log.Printf("%d lost the election.", rf.me)
	}
}

//
// the log entry type
//
type Entry struct {
	Command interface{}
	Term    int
	Index   int
}

//
// Leaders: Start the agreement when the leader receives client request.
// The leader appends the command to its log as a new entry (implemented in Start()),
// then issues AppendEntries RPCs in parallel to each of the other
// servers to replicate the entry. When the entry has been
// safely replicated (as described below), the leader applies
// the entry to its state machine and returns the result of that
// execution to the client. If followers crash or run slowly,
// or if network packets are lost, the leader retries Append-
// Entries RPCs indefinitely (even after it has responded to
// the client) until all followers eventually store all log entries.
//
func (rf *Raft) startAgreement() {
	finish := 1
	var mu sync.Mutex
	cond := sync.NewCond(&mu)

	rf.mu.Lock()
	term := rf.currentTerm
	rf.mu.Unlock()

	for i := 0; i < len(rf.peers); i++ {
		if rf.killed() {
			return
		}
		if i == rf.me {
			continue
		}
		go func(peer int) {
			for {
				// log.Printf("check state true for term %v, peer %v", term, peer)
				// If last log index ≥ nextIndex for a follower: send
				// AppendEntries RPC with log entries starting at nextIndex

				if rf.killed() {
					break
				}
				rf.mu.Lock()
				if rf.state != Leader {
					rf.mu.Unlock()
					break
				}
				if rf.currentTerm != term {
					rf.mu.Unlock()
					break
				}
				lastLogIndex := len(rf.log) - 1
				nextIndex := rf.nextIndex[peer]
				if lastLogIndex < nextIndex {
					rf.mu.Unlock()
					break
				}

				args := AppendEntriesArgs{
					Term:         rf.currentTerm,
					LeaderId:     rf.me,
					PrevLogIndex: rf.nextIndex[peer] - 1,
					PrevLogTerm:  rf.log[rf.nextIndex[peer]-1].Term,
					Entries:      rf.log[rf.nextIndex[peer]:],
					LeaderCommit: rf.commitIndex,
				}
				rf.mu.Unlock()
				reply := AppendEntriesReply{}

				ok := rf.sendAppendEntries(peer, &args, &reply)
				// Retry if not ok
				if !ok {
					time.Sleep(time.Duration(RetryReplicateEntryInterval) * time.Millisecond)
					continue
				}

				// If successful: update nextIndex and matchIndex for
				// follower (§5.3)
				if reply.Success {
					// log.Printf("%v reply AE success", peer)
					rf.mu.Lock()
					rf.nextIndex[peer] = args.Entries[len(args.Entries)-1].Index + 1
					rf.matchIndex[peer] = args.Entries[len(args.Entries)-1].Index
					rf.mu.Unlock()
				}

				// If AppendEntries fails because of log inconsistency:
				// decrement nextIndex and retry (§5.3)
				if !reply.Success && reply.Term <= args.Term {
					// log.Printf("%v reply AE fail due to log inconsistency", peer)
					// log.Printf("[%v] XTerm=%v, XIndex=%v, XLen=%v", peer, reply.XTerm, reply.XIndex, reply.XLen)
					rf.fastLogBackup(reply, peer)
				}
			}

			mu.Lock()
			defer mu.Unlock()
			finish++
			cond.Broadcast()
		}(i)
	}

	// If there exists an N such that N > commitIndex, a majority
	// of matchIndex[i] ≥ N, and log[N].term == currentTerm:
	// set commitIndex = N (§5.3, §5.4)
	mu.Lock()
	defer mu.Unlock()
	for finish != len(rf.peers)-1 {
		cond.Wait()

		rf.mu.Lock()
		for N := len(rf.log) - 1; N > rf.commitIndex; N-- {
			if rf.log[N].Term != rf.currentTerm {
				continue
			}
			count := 0
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					count++
					continue
				}
				if rf.matchIndex[i] >= N {
					count++
				}
			}
			if count >= len(rf.peers)/2+1 {
				// log.Printf("%v apply log", rf.me)
				rf.commitIndex = N
				rf.applyLog()
				// respond after entry applied to state machine (§5.3)
				// TO BE FINISHED: where to respond to client/tester (maybe don't need?)
				break
			}
		}
		rf.mu.Unlock()
	}
}

//
// Return last log index and term.
//
func (rf *Raft) getLastLog() (int, int) {
	index := len(rf.log) - 1
	term := rf.log[len(rf.log)-1].Term
	return index, term
}

//
// Fast log backup.
//
func (rf *Raft) fastLogBackup(reply AppendEntriesReply, peer int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Case 3: If XTerm is -1, follower doesn't have any entry at that index,
	// then set nextIndex as XLen
	if reply.XTerm == -1 || reply.XIndex == -1 {
		rf.nextIndex[peer] = reply.XLen
	} else {
		// Case 1: If leader doesn't have XTerm, then set nextIndex as XIndex
		// Case 2: If leader have XTerm, then set nextIndex as the last index
		// of entry of XTerm
		nextIndex := reply.XIndex
		for i := len(rf.log) - 1; i >= 1 && rf.log[i].Term >= reply.XTerm; i-- {
			if rf.log[i].Term == reply.XTerm {
				nextIndex = i
				break
			}
		}
		rf.nextIndex[peer] = nextIndex
	}
}
