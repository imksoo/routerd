// SPDX-License-Identifier: BSD-3-Clause

package main

import (
 "context"
 "database/sql"
 "errors"
 "io"
 "strings"
 "testing"
 "time"

 "github.com/imksoo/routerd/pkg/config"
)

func TestCanonicalBarrierRejectedCommitDoesNotRunCandidateEffects(t *testing.T) {
 router,opts:=faultApplyFixture(t)
 store:=faultGenerationStore(t,opts.StatePath,false)
 if err:=store.RecordEvent("test/v1","Sentinel","retained-before-commit","Normal","Retained","candidate must not prune this event");err!=nil{t.Fatal(err)}
 db,err:=sql.Open("sqlite",opts.StatePath);if err!=nil{t.Fatal(err)};defer db.Close()
 if _,err:=db.Exec(`UPDATE events SET created_at=? WHERE name=?`,time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano),"retained-before-commit");err!=nil{t.Fatal(err)}
 failure:=errors.New("canonical rename rejected")
 opts.attemptHooks.writeCanonical=func(string,[]byte)(config.AtomicWriteOutcome,error){return config.AtomicWriteOutcome{},failure}
 outcome,err:=runApplyChainOnceWithOutcome(context.Background(),router,opts,io.Discard,nil)
 if !errors.Is(err,failure)||outcome.Canonical!=canonicalNotReplaced {t.Fatalf("wrong failure outcome: %+v %v",outcome,err)}
 var count int
 if err:=db.QueryRow(`SELECT COUNT(*) FROM events WHERE name=?`,"retained-before-commit").Scan(&count);err!=nil{t.Fatal(err)}
 if count!=1 {t.Fatal("rejected canonical commit executed the real log-retention deletion")}
 if outcome.Stage!="canonical-write" {t.Fatalf("effect stage entered before canonical failure: %q",outcome.Stage)}
}

func TestCanonicalBarrierRequiresDurabilityAcknowledgement(t *testing.T) {
 router,opts:=faultApplyFixture(t)
 opts.attemptHooks.writeCanonical=func(string,[]byte)(config.AtomicWriteOutcome,error){return config.AtomicWriteOutcome{Replaced:true},nil}
 outcome,err:=runApplyChainOnceWithOutcome(context.Background(),router,opts,io.Discard,nil)
 if err==nil||outcome.DurabilityConfirmed||!strings.Contains(err.Error(),"apply failed") {t.Fatalf("unconfirmed canonical durability authorized apply: %+v %v",outcome,err)}
}
