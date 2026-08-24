package iroh

import (
	"testing"

	"github.com/pancsta/asyncmachine-go/pkg/rpc/test"
)

func TestSingleStateActive(t *testing.T) {
	test.TemplateTestSingleStateActive(t, newTest)
}

func TestMultipleStatesActive(t *testing.T) {
	test.TemplateTestMultipleStatesActive(t, newTest)
}

func TestExposeAllStateNames(t *testing.T) {
	test.TemplateTestExposeAllStateNames(t, newTest)
}

func TestStateSet(t *testing.T) {
	test.TemplateTestStateSet(t, newTest)
}

func TestStateAdd(t *testing.T) {
	test.TemplateTestStateAdd(t, newTest)
}

func TestStateRemove(t *testing.T) {
	test.TemplateTestStateRemove(t, newTest)
}

func TestRemoveRelation(t *testing.T) {
	test.TemplateTestRemoveRelation(t, newTest)
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	test.TemplateTestRemoveRelationSimultaneous(t, newTest)
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	test.TemplateTestRemoveRelationCrossBlocking(t, newTest)
}

func TestAddRelation(t *testing.T) {
	test.TemplateTestAddRelation(t, newTest)
}

func TestRequireRelation(t *testing.T) {
	test.TemplateTestRequireRelation(t, newTest)
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	test.TemplateTestRequireRelationWhenRequiredIsntActive(t, newTest)
}

func TestAutoStates(t *testing.T) {
	test.TemplateTestAutoStates(t, newTest)
}

func TestSwitch(t *testing.T) {
	test.TemplateTestSwitch(t, newTest)
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	test.TemplateTestRegressionRemoveCrossBlockedByImplied(t, newTest)
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	test.TemplateTestRegressionImpliedBlockByBeingRemoved(t, newTest)
}

func TestWhen2(t *testing.T) {
	test.TemplateTestWhen2(t, newTest)
}

func TestWhenActive(t *testing.T) {
	test.TemplateTestWhenActive(t, newTest)
}

func TestWhenNot2(t *testing.T) {
	test.TemplateTestWhenNot2(t, newTest)
}

func TestWhenNotActive(t *testing.T) {
	test.TemplateTestWhenNotActive(t, newTest)
}

func TestPartialAuto(t *testing.T) {
	test.TemplateTestPartialAuto(t, newTest)
}

func TestTime(t *testing.T) {
	test.TemplateTestTime(t, newTest)
}

func TestWhenTime(t *testing.T) {
	test.TemplateTestWhenTime(t, newTest)
}

func TestIs(t *testing.T) {
	test.TemplateTestIs(t, newTest)
}

func TestNot(t *testing.T) {
	test.TemplateTestNot(t, newTest)
}

func TestAny(t *testing.T) {
	test.TemplateTestAny(t, newTest)
}

func TestClock(t *testing.T) {
	test.TemplateTestClock(t, newTest)
}

func TestInspect(t *testing.T) {
	test.TemplateTestInspect(t, newTest)
}

func TestString(t *testing.T) {
	test.TemplateTestString(t, newTest)
}

func TestNestedMutation(t *testing.T) {
	test.TemplateTestNestedMutation(t, newTest)
}

func TestIsClock(t *testing.T) {
	test.TemplateTestIsClock(t, newTest)
}

func TestIsTime(t *testing.T) {
	test.TemplateTestIsTime(t, newTest)
}

func TestWhenQueue(t *testing.T) {
	test.TemplateTestWhenQueue(t, newTest)
}

func TestWhenQuery(t *testing.T) {
	test.TemplateTestWhenQuery(t, newTest)
}

func TestPipes(t *testing.T) {
	test.TemplateTestPipes(t, newTest)
}

func TestStateCtxBasic(t *testing.T) {
	test.TemplateTestStateCtxBasic(t, newTest)
}

func TestWhenTimeSum(t *testing.T) {
	test.TemplateTestWhenTimeSum(t, newTest)
}
