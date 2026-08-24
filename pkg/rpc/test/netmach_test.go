// TODO handle-bound tests

package test

import (
	"testing"
)

func TestSingleStateActive(t *testing.T) {
	TemplateTestSingleStateActive(t, newTest)
}

func TestMultipleStatesActive(t *testing.T) {
	TemplateTestMultipleStatesActive(t, newTest)
}

func TestExposeAllStateNames(t *testing.T) {
	TemplateTestExposeAllStateNames(t, newTest)
}

func TestStateSet(t *testing.T) {
	TemplateTestStateSet(t, newTest)
}

func TestStateAdd(t *testing.T) {
	TemplateTestStateAdd(t, newTest)
}

func TestStateRemove(t *testing.T) {
	TemplateTestStateRemove(t, newTest)
}

func TestRemoveRelation(t *testing.T) {
	TemplateTestRemoveRelation(t, newTest)
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	TemplateTestRemoveRelationSimultaneous(t, newTest)
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	TemplateTestRemoveRelationCrossBlocking(t, newTest)
}

func TestAddRelation(t *testing.T) {
	TemplateTestAddRelation(t, newTest)
}

func TestRequireRelation(t *testing.T) {
	TemplateTestRequireRelation(t, newTest)
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	TemplateTestRequireRelationWhenRequiredIsntActive(t, newTest)
}

func TestAutoStates(t *testing.T) {
	TemplateTestAutoStates(t, newTest)
}

func TestSwitch(t *testing.T) {
	TemplateTestSwitch(t, newTest)
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	TemplateTestRegressionRemoveCrossBlockedByImplied(t, newTest)
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	TemplateTestRegressionImpliedBlockByBeingRemoved(t, newTest)
}

func TestWhen2(t *testing.T) {
	TemplateTestWhen2(t, newTest)
}

func TestWhenActive(t *testing.T) {
	TemplateTestWhenActive(t, newTest)
}

func TestWhenNot2(t *testing.T) {
	TemplateTestWhenNot2(t, newTest)
}

func TestWhenNotActive(t *testing.T) {
	TemplateTestWhenNotActive(t, newTest)
}

func TestPartialAuto(t *testing.T) {
	TemplateTestPartialAuto(t, newTest)
}

func TestTime(t *testing.T) {
	TemplateTestTime(t, newTest)
}

func TestWhenTime(t *testing.T) {
	TemplateTestWhenTime(t, newTest)
}

func TestIs(t *testing.T) {
	TemplateTestIs(t, newTest)
}

func TestNot(t *testing.T) {
	TemplateTestNot(t, newTest)
}

func TestAny(t *testing.T) {
	TemplateTestAny(t, newTest)
}

func TestClock(t *testing.T) {
	TemplateTestClock(t, newTest)
}

func TestInspect(t *testing.T) {
	TemplateTestInspect(t, newTest)
}

func TestString(t *testing.T) {
	TemplateTestString(t, newTest)
}

func TestNestedMutation(t *testing.T) {
	TemplateTestNestedMutation(t, newTest)
}

func TestIsClock(t *testing.T) {
	TemplateTestIsClock(t, newTest)
}

func TestIsTime(t *testing.T) {
	TemplateTestIsTime(t, newTest)
}

func TestWhenQueue(t *testing.T) {
	TemplateTestWhenQueue(t, newTest)
}

func TestWhenQuery(t *testing.T) {
	TemplateTestWhenQuery(t, newTest)
}

func TestPipes(t *testing.T) {
	TemplateTestPipes(t, newTest)
}

func TestStateCtxBasic(t *testing.T) {
	TemplateTestStateCtxBasic(t, newTest)
}

func TestWhenTimeSum(t *testing.T) {
	TemplateTestWhenTimeSum(t, newTest)
}
