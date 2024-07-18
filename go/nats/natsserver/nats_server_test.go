package natsserver

import (
	"testing"
)

func TestServer(t *testing.T) {
	natserver := NewNatsServer(UserName, PassWord)
	err := natserver.Start()
	if err != nil {
		t.Fatalf("natserver start failed, err:%v", err)
	}
}

func TestStop(t *testing.T) {
	natserver := NewNatsServer(UserName, PassWord)
	err := natserver.Start()
	if err != nil {
		t.Fatalf("natserver start failed, err:%v", err)
	}
	err = natserver.Stop()
	if err != nil {
		t.Fatalf("natserver stop failed, err:%v", err)
	}
}

func TestStartMonitorClient(t *testing.T) {
	natserver := NewNatsServer(UserName, PassWord)
	err := natserver.Start()
	if err != nil {
		t.Fatalf("natserver start failed, err:%v", err)
	}
	err = natserver.StartMonitorClient()
	if err != nil {
		t.Fatalf("natserver start monitor client failed, err:%v", err)
	}
}

func TestStopMonitorClient(t *testing.T) {
	natserver := NewNatsServer(UserName, PassWord)
	err := natserver.Start()
	if err != nil {
		t.Fatalf("natserver start failed, err:%v", err)
	}
	err = natserver.StartMonitorClient()
	if err != nil {
		t.Fatalf("natserver start monitor client failed, err:%v", err)
	}
	err = natserver.StopMonitorClient()
	if err != nil {
		t.Fatalf("natserver stop monitor client failed, err:%v", err)
	}
}
