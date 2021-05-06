// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016 Datadog, Inc.

package profiler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime/pprof"
	"time"

	"github.com/DataDog/gostackparse"
	"github.com/felixge/pprofutils"
	pprofile "github.com/google/pprof/profile"
)

// ProfileType represents a type of profile that the profiler is able to run.
type ProfileType int

const (
	// HeapProfile reports memory allocation samples; used to monitor current
	// and historical memory usage, and to check for memory leaks.
	HeapProfile ProfileType = iota
	// CPUProfile determines where a program spends its time while actively consuming
	// CPU cycles (as opposed to while sleeping or waiting for I/O).
	CPUProfile
	// BlockProfile shows where goroutines block waiting on synchronization primitives
	// (including timer channels). Block profile is not enabled by default.
	BlockProfile
	// MutexProfile reports the lock contentions. When you think your CPU is not fully utilized due
	// to a mutex contention, use this profile. Mutex profile is not enabled by default.
	MutexProfile
	// GoroutineProfile reports stack traces of all current goroutines
	GoroutineProfile
	// expGoroutineWaitProfile reports stack traces and wait durations for
	// goroutines that have been waiting or blocked by a syscall for > 1 minute
	// since the last GC. This feature is currently experimental and only
	// available within DD by setting the DD_PROFILING_WAIT_PROFILE env variable.
	expGoroutineWaitProfile
	// MetricsProfile reports top-line metrics associated with user-specified profiles
	MetricsProfile
)

// collector holds the implementation details of a ProfileType, see collectors
// map below.
type collector struct {
	// Name specifies the profile name as used with pprof.Lookup(name) (in
	// collectGenericProfile) and returned by ProfileType.String(). For profile
	// types that don't use this approach (e.g. cpu) the name isn't used for
	// anything.
	Name string
	// Filename is the filename used for uploading the profile to the datadog
	// backend which is aware of them. Delta profiles are prefixed with "delta-"
	// automatically. In theory this could be derrived from the Name field, but
	// this isn't done due to idiosyncratic filename used by the
	// GoroutineProfile.
	Filename string
	// Delta controls if this profile should be generated as a delta profile.
	// This is useful for profiles that represent samples collected over the
	// lifetime of the process (i.e. heap, block, mutex). If nil, no delta
	// profile is generated.
	Delta *pprofutils.Delta
	// Collect collects the given profile and returns the data for it. Most
	// profiles will be in pprof format, i.e. gzip compressed proto buf data.
	Collect func(collector, *profiler) ([]byte, error)
}

// collectors map every ProfileType to a collector implementation.
var collectors = map[ProfileType]collector{
	CPUProfile: {
		Name:     "cpu",
		Filename: "cpu.pprof",
		Collect: func(_ collector, p *profiler) ([]byte, error) {
			var buf bytes.Buffer
			if err := startCPUProfile(&buf); err != nil {
				return nil, err
			}
			time.Sleep(p.cfg.cpuDuration)
			stopCPUProfile()
			return buf.Bytes(), nil
		},
	},
	// HeapProfile is complex due to how the Go runtime exposes it. It contains 4
	// sample types alloc_objects/count, alloc_space/bytes, inuse_objects/count,
	// inuse_space/bytes. The first two represent allocations over the lifetime
	// of the process, so we do delta profiling for them. The last two are
	// snapshots of the current heap state, so we leave them as-is.
	HeapProfile: {
		Name:     "heap",
		Filename: "heap.pprof",
		Delta: &pprofutils.Delta{SampleTypes: []pprofutils.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
		}},
		Collect: collectGenericProfile,
	},
	MutexProfile: {
		Name:     "mutex",
		Filename: "mutex.pprof",
		// MutexProfile in Go is lifetime of the process, so we compute delta
		// profiles.
		Delta:   &pprofutils.Delta{},
		Collect: collectGenericProfile,
	},
	BlockProfile: {
		Name:     "block",
		Filename: "block.pprof",
		// BlockProfile in Go is lifetime of the process, so we compute delta
		// profiles.
		Delta:   &pprofutils.Delta{},
		Collect: collectGenericProfile,
	},
	GoroutineProfile: {
		Name:     "goroutine",
		Filename: "goroutines.pprof",
		Collect:  collectGenericProfile,
	},
	expGoroutineWaitProfile: {
		Name:     "goroutinewait",
		Filename: "goroutineswait.pprof",
		Collect: func(c collector, _ *profiler) ([]byte, error) {
			var (
				now   = now()
				text  = &bytes.Buffer{}
				pprof = &bytes.Buffer{}
			)
			if err := lookupProfile(c.Name, text, 2); err != nil {
				return nil, err
			}
			err := goroutineDebug2ToPprof(text, pprof, now)
			return pprof.Bytes(), err
		},
	},
	MetricsProfile: {
		Name:     "metrics",
		Filename: "metrics.json",
		Collect: func(_ collector, p *profiler) ([]byte, error) {
			var buf bytes.Buffer
			err := p.met.report(now(), &buf)
			return buf.Bytes(), err
		},
	},
}

func collectGenericProfile(c collector, _ *profiler) ([]byte, error) {
	var buf bytes.Buffer
	err := lookupProfile(c.Name, &buf, 0)
	return buf.Bytes(), err
}

// collector returns the collector for this profileType.
func (t ProfileType) collector() collector {
	pt, ok := collectors[t]
	if ok {
		return pt
	}
	return collector{
		Name:     "unknown",
		Filename: "unknown",
		Collect: func(_ collector, _ *profiler) ([]byte, error) {
			return nil, errors.New("profile type not implemented")
		},
	}
}

// String returns the name of the profile.
func (t ProfileType) String() string {
	return t.collector().Name
}

// Filename is the identifier used on upload.
func (t ProfileType) Filename() string {
	return t.collector().Filename
}

// Tag used on profile metadata
func (t ProfileType) Tag() string {
	return fmt.Sprintf("profile_type:%s", t)
}

// profile specifies a profiles data (gzipped protobuf, json), and the types contained within it.
type profile struct {
	// name indicates profile type and format (e.g. cpu.pprof, metrics.json)
	name string
	data []byte
}

// batch is a collection of profiles of different types, collected at roughly the same time. It maps
// to what the Datadog UI calls a profile.
type batch struct {
	start, end time.Time
	host       string
	profiles   []*profile
}

func (b *batch) addProfile(p *profile) {
	b.profiles = append(b.profiles, p)
}

func (p *profiler) runProfile(t ProfileType) ([]*profile, error) {
	var (
		pt    = t.collector()
		start = now()
	)
	data, err := pt.Collect(pt, p)
	if err != nil {
		return nil, err
	}

	profs := []*profile{{
		name: pt.Filename,
		data: data,
	}}

	if prof, err := p.deltaProfile(pt, t, data); err != nil {
		return nil, fmt.Errorf("delta profile error: %s", err)
	} else if prof != nil {
		profs = append(profs, prof)
	}

	end := now()
	tags := append(p.cfg.tags, t.Tag())
	p.cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)

	return profs, nil
}

func (p *profiler) deltaProfile(c collector, t ProfileType, data []byte) (*profile, error) {
	pt := c
	if pt.Delta == nil {
		return nil, nil
	}

	currentProf, err := pprofile.ParseData(data)
	if err != nil {
		return nil, fmt.Errorf("delta prof parse: %v", err)
	}
	var deltaData []byte
	if prevProf := p.prev[t]; prevProf == nil {
		// First time we collect t there is no previous profile.
		deltaData = data
	} else {
		// TODO text
		// https://github.com/golang/go/commit/2ff1e3ebf5de77325c0e96a6c2a229656fc7be50#diff-94594f8f13448da956b02997e50ca5a156b65085993e23bbfdda222da6508258R303-R304
		deltaProf, err := pt.Delta.Convert(prevProf, currentProf)
		if err != nil {
			return nil, fmt.Errorf("delta prof merge: %v", err)
		}
		deltaProf.DurationNanos = currentProf.TimeNanos - prevProf.TimeNanos
		deltaProf.TimeNanos = currentProf.TimeNanos
		deltaBuf := &bytes.Buffer{}
		if err := deltaProf.Write(deltaBuf); err != nil {
			return nil, fmt.Errorf("delta prof write: %v", err)
		}
		deltaData = deltaBuf.Bytes()
	}
	// Keep the most recent profile in memory for future diffing. This needs to
	// be taken into account when enforcing memory limits going forward.
	p.prev[t] = currentProf
	return &profile{
		// TODO(fg) are those good filenames? Is there a better way to flag
		// these profiles for the backend?
		name: "delta-" + pt.Filename,
		data: deltaData,
	}, nil
}

var (
	// startCPUProfile starts the CPU profile; replaced in tests
	startCPUProfile = pprof.StartCPUProfile
	// stopCPUProfile stops the CPU profile; replaced in tests
	stopCPUProfile = pprof.StopCPUProfile
)

// lookpupProfile looks up the profile with the given name and writes it to w. It returns
// any errors encountered in the process. It is replaced in tests.
var lookupProfile = func(name string, w io.Writer, debug int) error {
	prof := pprof.Lookup(name)
	if prof == nil {
		return errors.New("profile not found")
	}
	return prof.WriteTo(w, debug)
}

func goroutineDebug2ToPprof(r io.Reader, w io.Writer, t time.Time) (err error) {
	// gostackparse.Parse() has been extensively tested and should not crash
	// under any circumstances, but we really want to avoid crashing a customers
	// applications, so this code will recover from any unexpected panics and
	// return them as an error instead.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	goroutines, errs := gostackparse.Parse(r)

	functionID := uint64(1)
	locationID := uint64(1)

	p := &pprofile.Profile{
		TimeNanos: t.UnixNano(),
	}
	m := &pprofile.Mapping{ID: 1, HasFunctions: true}
	p.Mapping = []*pprofile.Mapping{m}
	p.SampleType = []*pprofile.ValueType{
		{
			Type: "waitduration",
			Unit: "nanoseconds",
		},
	}

	for _, g := range goroutines {
		sample := &pprofile.Sample{
			Value: []int64{g.Wait.Nanoseconds()},
			Label: map[string][]string{
				"state":   {g.State}, // TODO(fg) split into atomicstatus/waitreason?
				"lockedm": {fmt.Sprintf("%t", g.LockedToThread)},
			},
			NumUnit:  map[string][]string{"goid": {"id"}},
			NumLabel: map[string][]int64{"goid": {int64(g.ID)}},
		}

		// Treat the frame that created this goroutine as part of the stack so it
		// shows up in the stack trace / flame graph. Hopefully this will be more
		// useful than confusing for people.
		if g.CreatedBy != nil {
			// TODO(fg) should we modify the function name to include "created by"?
			g.Stack = append(g.Stack, g.CreatedBy)
		}

		// Based on internal discussion, the current strategy is to use virtual
		// frames to indicate truncated stacks, see [1] for how python/jd does it.
		// [1] https://github.com/DataDog/dd-trace-py/blob/e933d2485b9019a7afad7127f7c0eb541341cdb7/ddtrace/profiling/exporter/pprof.pyx#L117-L121
		if g.FramesElided {
			g.Stack = append(g.Stack, &gostackparse.Frame{
				Func: "...additional frames elided...",
			})
		}

		for _, call := range g.Stack {
			function := &pprofile.Function{
				ID:       functionID,
				Name:     call.Func,
				Filename: call.File,
			}
			p.Function = append(p.Function, function)
			functionID++

			location := &pprofile.Location{
				ID:      locationID,
				Mapping: m,
				Line: []pprofile.Line{{
					Function: function,
					Line:     int64(call.Line),
				}},
			}
			p.Location = append(p.Location, location)
			locationID++

			sample.Location = append(sample.Location, location)
		}

		p.Sample = append(p.Sample, sample)
	}

	// Put the error message in the pprof profiles as comments in case we need to
	// debug issues at some point.
	// TODO(fg) would be nice to also have a metric counter for this
	for _, err := range errs {
		p.Comments = append(p.Comments, "error: "+err.Error())
	}

	if err := p.CheckValid(); err != nil {
		return fmt.Errorf("marshalGoroutineDebug2Profile: %s", err)
	} else if err := p.Write(w); err != nil {
		return fmt.Errorf("marshalGoroutineDebug2Profile: %s", err)
	}
	return nil
}

// now returns current time in UTC.
func now() time.Time {
	return time.Now().UTC()
}
