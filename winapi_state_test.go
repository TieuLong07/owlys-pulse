package main

import (
	"errors"
	"reflect"
	"testing"
)

type fakeSettings struct {
	values map[string]settingValue
	writes []string
	fail   string
}

func (f *fakeSettings) read(k string) (settingValue, error) {
	v, ok := f.values[k]
	if !ok {
		return settingValue{}, nil
	}
	return v, nil
}
func (f *fakeSettings) write(k string, v settingValue) error {
	if k == f.fail {
		return errors.New("write failed")
	}
	if !v.Present {
		delete(f.values, k)
		f.writes = append(f.writes, "delete:"+k)
	} else {
		f.values[k] = v
		f.writes = append(f.writes, "set:"+k)
	}
	return nil
}

func TestSettingSnapshotRestoresAbsentAndOriginalTypes(t *testing.T) {
	f := &fakeSettings{values: map[string]settingValue{"a": {Present: true, Type: 4, Data: []byte{9, 0, 0, 0}}}}
	before, err := snapshotSettings(f, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.write("a", settingValue{Present: true, Type: 1, Data: []byte{72, 0}})
	_ = f.write("b", settingValue{Present: true, Type: 4, Data: []byte{8, 0, 0, 0}})
	if err := restoreSettings(f, before); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, map[string]settingValue{"a": {Present: true, Type: 4, Data: []byte{9, 0, 0, 0}}}) {
		t.Fatalf("incorrect restoration: %#v", f.values)
	}
}

func TestExtremePowerRestoresCapturedSchemeAndValues(t *testing.T) {
	f := &fakePower{scheme: "abc", values: map[string]uint32{"CPMINCORES": 35, "PROCTHROTTLEMIN": 12}}
	state, err := applyExtremePower(f)
	if err != nil {
		t.Fatal(err)
	}
	if f.values["CPMINCORES"] != 100 || f.values["PROCTHROTTLEMIN"] != 100 {
		t.Fatalf("boost failed: %v", f.values)
	}
	f.scheme = "other"
	if err := restoreExtremePower(f, state); err != nil {
		t.Fatal(err)
	}
	if f.scheme != "other" || f.values["CPMINCORES"] != 35 || f.values["PROCTHROTTLEMIN"] != 12 {
		t.Fatalf("restoration targeted wrong scheme: %v %v", f.scheme, f.values)
	}
}

type fakePower struct {
	scheme string
	values map[string]uint32
}

func (f *fakePower) currentScheme() (string, error)            { return f.scheme, nil }
func (f *fakePower) readAC(scheme, key string) (uint32, error) { return f.values[key], nil }
func (f *fakePower) writeAC(scheme, key string, value uint32) error {
	f.values[key] = value
	return nil
}
func (f *fakePower) activate(scheme string) error { return nil }

func TestForegroundRestoresOriginalPriorities(t *testing.T) {
	f := &fakePriority{pid: 42, class: 32, io: 2, page: 3}
	state, err := applyForegroundPriority(f)
	if err != nil {
		t.Fatal(err)
	}
	if f.class != highPriorityClass || f.io != 3 || f.page != 5 {
		t.Fatalf("boost failed: %#v", f)
	}
	if err := restoreForegroundPriority(f, state); err != nil {
		t.Fatal(err)
	}
	if f.class != 32 || f.io != 2 || f.page != 3 {
		t.Fatalf("restoration failed: %#v", f)
	}
}

type fakePriority struct {
	pid             uint32
	class, io, page uint32
}

func (f *fakePriority) foregroundPID() (uint32, error) { return f.pid, nil }
func (f *fakePriority) readPriorities(pid uint32) (uint32, uint32, uint32, error) {
	return f.class, f.io, f.page, nil
}
func (f *fakePriority) writeClass(pid, value uint32) error { f.class = value; return nil }
func (f *fakePriority) writeIO(pid, value uint32) error    { f.io = value; return nil }
func (f *fakePriority) writePage(pid, value uint32) error  { f.page = value; return nil }

func TestMMCSSApplyChangesAllSixAndRestoresAbsent(t *testing.T) {
	f := &fakeSettings{values: map[string]settingValue{"Games/Priority": {Present: true, Type: 4, Data: []byte{2, 0, 0, 0}}}}
	saved, err := applyMMCSSSettings(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 6 || len(f.values) != 6 {
		t.Fatalf("incomplete snapshot or writes: %d, %d", len(saved), len(f.values))
	}
	if err := restoreSettings(f, saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.values, map[string]settingValue{"Games/Priority": {Present: true, Type: 4, Data: []byte{2, 0, 0, 0}}}) {
		t.Fatalf("failed to remove originally absent keys: %#v", f.values)
	}
}

func TestMMCSSApplyFailureRollsBack(t *testing.T) {
	f := &fakeSettings{values: map[string]settingValue{}, fail: "Games/Priority"}
	_, err := applyMMCSSSettings(f)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(f.values) != 0 {
		t.Fatalf("partial boost remained: %v", f.values)
	}
}

func TestSettingSnapshotFailureDoesNotReturnPartialSnapshot(t *testing.T) {
	f := &fakeSettings{values: map[string]settingValue{}}
	f.fail = "b"
	snap := []savedSetting{{Key: "a", Value: settingValue{Present: false}}, {Key: "b", Value: settingValue{Present: false}}}
	if err := restoreSettings(f, snap); err == nil {
		t.Fatal("expected failure")
	}
	if len(f.writes) != 1 || f.writes[0] != "delete:a" {
		t.Fatalf("did not continue to second setting: %v", f.writes)
	}
}
