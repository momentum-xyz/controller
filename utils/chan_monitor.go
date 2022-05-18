package utils

import (
	"reflect"
	"time"
)

func ChanMonitor(name string, ch interface{}, intvl time.Duration) {
	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan {
		log.Warnf("chan log: not a chaing to monitor: %s", name)
		return
	}

	log.Infof("chan log: request to monitor: %s", name)

	c := v.Cap()
	if c == 0 {
		log.Warnf("chan log: unbuffered chan: %s", name)
		return
	}
	for {
		if l := v.Len(); l == c {
			log.Warnf("chan log: chan %s is full: len(%d) cap(%d)", name, l, c)
		}
		time.Sleep(intvl)
	}
}

func ChanMonitorEmpty(name string, ch interface{}, intvl time.Duration) {
	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan {
		log.Warnf("chan log: not a chaing to monitor: %s", name)
		return
	}

	log.Infof("chan log: request to monitor: %s", name)

	c := v.Cap()
	if c == 0 {
		log.Warnf("chan log: unbuffered chan: %s", name)
		return
	}
	for {
		if l := v.Len(); l == 0 {
			log.Warnf("chan log: chan %s is full : len(%d) cap(%d)", name, l, c)
		}
		time.Sleep(intvl)
	}
}
