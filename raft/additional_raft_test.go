package raft

import (
	"math/rand"
	"testing"
)

func TestRequestVote3A(t *testing.T) {
	// Test: Reply false if term < currentTerm
	rf1 := &Raft{
		me:          0,
		dead:        0,
		state:       Follower,
		currentTerm: 2,
		log:         make([]Entry, 0),
	}
	rf1.log = append(rf1.log, Entry{Term: 0, Index: 0})

	args1 := &RequestVoteArgs{
		Term:         1,
		CandidateId:  1,
		LastLogIndex: 1,
		LastLogTerm:  1,
	}
	reply1 := &RequestVoteReply{}
	rf1.RequestVote(args1, reply1)
	if reply1.VoteGranted {
		t.Fatal("Should reply false but not")
	}

	// Test: If votedFor is not null or candidateId, not grant vote
	rf3 := &Raft{
		me:          0,
		dead:        0,
		state:       Follower,
		currentTerm: 1,
		log:         make([]Entry, 0),
		votedFor:    0,
	}
	rf3.log = append(rf3.log, Entry{Term: 0, Index: 0})

	args3 := &RequestVoteArgs{
		Term:         1,
		CandidateId:  1,
		LastLogIndex: 1,
		LastLogTerm:  1,
	}
	reply3 := &RequestVoteReply{}
	rf3.RequestVote(args3, reply3)
	if reply3.VoteGranted {
		t.Fatal("Should reply false but not")
	}
}

func TestKilled3A(t *testing.T) {
	rf := &Raft{
		dead: 0,
	}
	if rf.killed() {
		t.Fatal("Should be alive but not")
	}
	rf.Kill()
	if !rf.killed() {
		t.Fatal("Should be killed but not")
	}
}

func TestAppendEntries3B(t *testing.T) {
	// Test: 1. Reply false if term < currentTerm
	rf := &Raft{
		me:          0,
		dead:        0,
		state:       Follower,
		currentTerm: 2,
		log:         make([]Entry, 0),
	}
	rf.log = append(rf.log, Entry{Term: 0, Index: 0})

	args := &AppendEntriesArgs{
		Term:         1,
		LeaderId:     1,
		PrevLogIndex: 1,
		PrevLogTerm:  1,
	}
	reply := &AppendEntriesReply{}
	rf.AppendEntries(args, reply)
	if reply.Success {
		t.Fatal("Should reply false but not")
	}

	// Test: 2. Reply false if log doesn’t contain an entry at prevLogIndex
	// whose term matches prevLogTerm
	rf = &Raft{
		me:          0,
		dead:        0,
		state:       Follower,
		currentTerm: 1,
		log:         make([]Entry, 0),
	}
	rf.log = append(rf.log, Entry{Term: 0, Index: 0})

	args = &AppendEntriesArgs{
		Term:         1,
		LeaderId:     1,
		PrevLogIndex: 1,
		PrevLogTerm:  1,
	}
	reply = &AppendEntriesReply{}
	rf.AppendEntries(args, reply)
	if reply.Success {
		t.Fatal("Should reply false but not")
	}

	// Test: 3. Else, reply true.
	rf = &Raft{
		me:          0,
		dead:        0,
		state:       Follower,
		currentTerm: 1,
		log:         make([]Entry, 0),
	}
	rf.log = append(rf.log, Entry{Term: 0, Index: 0})
	rf.log = append(rf.log, Entry{Term: 1, Index: 1})

	args = &AppendEntriesArgs{
		Term:         1,
		LeaderId:     1,
		PrevLogIndex: 1,
		PrevLogTerm:  1,
	}
	reply = &AppendEntriesReply{}
	rf.AppendEntries(args, reply)
	if !reply.Success {
		t.Fatal("Should reply true but not")
	}
}

func TestLogFastBackUp(t *testing.T) {
	rf := &Raft{
		me:          0,
		dead:        0,
		state:       Leader,
		currentTerm: 4,
		log:         make([]Entry, 0),
		nextIndex:   make([]int, 2),
	}
	rf.log = append(rf.log, Entry{Term: 0, Index: 0})
	rf.log = append(rf.log, Entry{Term: 1, Index: 1})
	rf.log = append(rf.log, Entry{Term: 2, Index: 2})
	rf.log = append(rf.log, Entry{Term: 2, Index: 3})
	rf.log = append(rf.log, Entry{Term: 4, Index: 4})
	// Case 1: If leader doesn't have XTerm, then set nextIndex as XIndex
	rf.nextIndex[1] = 5
	reply := AppendEntriesReply{
		Term:    1,
		Success: false,
		XTerm:   3,
		XIndex:  2,
		XLen:    -1,
	}
	rf.fastLogBackup(reply, 1)
	if rf.nextIndex[1] != 2 {
		t.Fatalf("NextIndex should be %v but is %v", reply.XIndex, rf.nextIndex[1])
	}
	// Case 2: If leader have XTerm, then set nextIndex as the last index
	// of entry of XTerm
	rf.nextIndex[1] = 5
	reply = AppendEntriesReply{
		Term:    1,
		Success: false,
		XTerm:   2,
		XIndex:  2,
		XLen:    -1,
	}
	rf.fastLogBackup(reply, 1)
	if rf.nextIndex[1] != 3 {
		t.Fatalf("NextIndex should be %v but is %v", reply.XIndex, rf.nextIndex[1])
	}

	// Case 3: If XTerm is -1, follower doesn't have any entry at that index,
	// then set nextIndex as XLen
	rf.nextIndex[1] = 5
	reply = AppendEntriesReply{
		Term:    1,
		Success: false,
		XTerm:   -1,
		XIndex:  -1,
		XLen:    3,
	}
	rf.fastLogBackup(reply, 1)
	if rf.nextIndex[1] != 3 {
		t.Fatalf("NextIndex should be %v but is %v", reply.XIndex, rf.nextIndex[1])
	}
}

func TestLargeElections3A(t *testing.T) {
	servers := 15
	cfg := make_config(t, servers, false, false)
	defer cfg.cleanup()

	cfg.begin("Test (3A): multiple elections unreliable")

	cfg.checkOneLeader()

	iters := 10
	for ii := 1; ii < iters; ii++ {
		// disconnect three nodes
		i1 := rand.Int() % servers
		i2 := rand.Int() % servers
		i3 := rand.Int() % servers
		cfg.disconnect(i1)
		cfg.disconnect(i2)
		cfg.disconnect(i3)

		// either the current leader should still be alive,
		// or the remaining four should elect a new one.
		cfg.checkOneLeader()

		cfg.connect(i1)
		cfg.connect(i2)
		cfg.connect(i3)
	}

	cfg.checkOneLeader()

	cfg.end()
}
