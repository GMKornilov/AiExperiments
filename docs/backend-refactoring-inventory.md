# Инвентарь legacy и границ рефакторинга

Активный composition root: `backend/cmd/api-server/main.go`.
`memory.Store` удалён; его роли принадлежат `application/workspace`,
`application/conversation`, `application/taskflow`, `application/state` и
`adapters/statejson`. HTTP и disk DTO раздельны; domain не импортирует I/O.

`httpapi.Handler` перенесён в `legacy_handler_test.go`: в сервер не компилируется.
`session.Store` и `agent.Conversation` сохранены для исторических regression
проверок. У них нет вызывающего production-кода в активном графе API.
Их полное удаление — отдельная граница retirement, как требует этап 8 плана;
подменять ими новый acceptance gate нельзя. `agent.Provider`, snapshots и
OpenAI transport используются адаптером; их удаление не входит в retirement.

Общие `decode`/`method` находятся в `httpapi/transport.go` и проверяются также
активными API tests. SnapshotLoader перенесён в `adapters/configsnapshot`;
legacy wrapper остался только в test-файле.

## Перенос полезных контрактов memory.Store

| Старые проверки | Активная замена |
| --- | --- |
| Send ordering, prompt window, profile precedence/privacy | `TestOrdinaryResponseWaitsForMemoryAndTitleDoesNotBlock`, `TestPromptWindowProfileAndExtractorPrivacy` |
| Main failure/retry, owner isolation, HTTP validation, admin | `memory_handler_test.go`, `TestRepositoryContracts/ownership-and-copy`, browser memory suite |
| Extractor errors/JSON, old snapshots and pair | `TestRepositoryContracts/normal-vs-task-extractor-failure`, `TestFactsStrictShape`, `compose-state.mjs` |
| Profile create/select/delete/default, Unicode, rollback | `TestProfileValidationAndDeleteRollback`, active API profile test, browser profiles suite |
| Rename/clear/delete/create storage rollback | `TestRepositoryContracts/all-workspace-failures-rollback` (fake + JSON) |
| Current JSON migration, corrupt/unknown data, restart | `statejson/repository_test.go`, process restart integration, Docker recreation/SIGKILL |
| Title once/latency/fallback/Unicode/restart | `TestRepositoryContracts/title-independent-first-accept`, `TestTitleFallbackIsDurableAndNotRepeated`, `TestPendingTitleNormalizesWithoutSecondCall` |
| Task clarification/plan/validation/pause/provider failure | `TestProposalInvariants`, `TestRepositoryContracts`, `TestRestartInFlightAtEveryStage`, tasks browser suite |
| Устаревший title-after-pair и phrase-based clarify | Заменены спецификационными assertions; прежняя ошибочная семантика не сохраняется |

## Поимённый инвентарь оставшегося legacy

В таблицах перечислены объявления и тесты. «Сохранить» означает regression
зависимость; активный эквивалент — указанный use case, если сценарий ещё входит
в продукт. Strategy/summary/branching относятся к deprecated specs.

| Типы | Текущий потребитель | Решение / активный эквивалент |
| --- | --- | --- |
| HTTP `Store`, `Handler`, `statusWriter`, `sendRequest`, `eventRequest`, `messageDTO`, `dialogDTOType` | Только legacy HTTP tests | Test-only; новые endpoint interfaces, transport и DTO в `httpapi` |
| Session `Dialog`, `Branch`, `Listing`, `storedDialog`, `storedBranch`, `browserSession`, `Store`, `AttemptObserver` | Legacy HTTP/session tests | Сохранены до отдельного retirement PR; активное состояние в `domain/model`, операции в application |
| Agent `Conversation`, `Attempt`, `Message`, `Status` | Legacy session/agent tests | Сохранены до retirement; активные orchestration в conversation/taskflow |
| Agent `Provider`, `Snapshot`, `DialogSnapshot`, `ErrorCategory`, `AttemptError` | `adapters/openai`, `adapters/configsnapshot`, существующий transport | Сохранить активные provider/snapshot/error контракты |
| Agent `ContextStrategy`, `FactsConfig` | Совместимый config/snapshot и legacy tests | Сохранить до отдельного решения по deprecated config; не использовать как активные use cases |

### HTTP

| Файл | Объявление | Решение / эквивалент |
| --- | --- | --- |
| `contract_test.go` | `p *blockingProvider.Complete` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `contract_test.go` | `TestHTTPConversationContract` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `contract_test.go` | `TestAcceptedDisconnectReconcilesWithoutDuplicate` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `contract_test.go` | `TestTemperatureSnapshotsReachChatAndText` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `contract_test.go` | `writeFixture` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `contract_test.go` | `nestedConfig` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `contract_test.go` | `createDialog` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `contract_test.go` | `sendMessage` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Create` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.List` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Get` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Exists` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Select` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Delete` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Send` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `s *fakeStore.Retry` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `handler_test.go` | `TestEventsRejectForeignDialog` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `handler_test.go` | `TestAdminUnknownStaysUnknown` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `handler_test.go` | `TestInvalidOwnedMessageIsLoggedForDialog` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `large_prompt_test.go` | `TestLargePromptReachesProviderUnchanged` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `large_prompt_test.go` | `testLargePrompt` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `w *statusWriter.WriteHeader` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `w *statusWriter.Write` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `New` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `SnapshotLoader` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.ServeHTTP` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.dialogs` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.contextStrategies` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.dialog` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.selectDialog` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.strategy` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.branches` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.selectBranch` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.send` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.retry` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.sendError` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `attemptOutcome` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.events` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.admin` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.log` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.logContext` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.ok` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.fail` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `dialogDTO` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `listingDTO` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `legacy_handler_test.go` | `h *Handler.compact` | HTTP DTO/error/transport tests; workspace/conversation/taskflow |
| `tokens_test.go` | `TestHTTPDialogExposesUsageWithoutInternalLedger` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `trace_test.go` | `TestJournalCapturesChatTitleAndSummary` | Сохранить тест; исторический gate, если обращается к /api/dialogs |

### Session

| Файл | Объявление | Решение / эквивалент |
| --- | --- | --- |
| `compression_test.go` | `TestOldDialogReceivesSummaryConfigurationOnRestart` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `compression_test.go` | `TestCompressionPersistsAndRemainsSessionScoped` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `compression_test.go` | `TestLegacySummaryPrunesOriginalHistory` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `p *contextProvider.Complete` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `p *contextProvider.CompleteWithUsage` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `p *contextProvider.callsFor` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `contextStrategySnapshot` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `TestParseFactsRejectsAnythingButOneFlatUniqueStringObject` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestSlidingWindowSendsCurrentInputOnceAndNeverSendsArchive` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestFactsCorrectionPrecedesChatAndChatRetryDoesNotReextract` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestInvalidFactsResponseKeepsPriorSnapshotAndDoesNotCallChat` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestFactsFailureRemainsRetryableAfterRestart` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestSlidingAndFactsRestartAfterPruningRetainTranscriptAndBoundedMemory` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestSummaryCannotChangeStrategyAfterCompactEmptiesAgentMemory` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestBranchChildrenAreIsolatedAndChargeSharedPrefixOnce` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestBranchSelectRejectsConcurrentAttemptWithoutLeakingAnswer` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `TestRestartRestoresActiveBranchWithoutSiblingTranscript` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `context_strategies_test.go` | `messageTexts` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `p *branchBlockingProvider.Complete` | workspace/conversation/taskflow + statejson |
| `context_strategies_test.go` | `TestContextStrategySnapshotsHaveUsableTimeout` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence.go` | `dialogDirectory` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `validDialogID` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.readState` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `OpenStore` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `restoreConversation` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.StorageError` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.RegisterSecrets` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.saveLocked` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.writeStateLocked` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `s *Store.removeOrphanFiles` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `replaceFile` | workspace/conversation/taskflow + statejson |
| `persistence.go` | `logStorage` | workspace/conversation/taskflow + statejson |
| `persistence_test.go` | `openTestStore` | workspace/conversation/taskflow + statejson |
| `persistence_test.go` | `TestPersistenceRestartRestoresHistoryContextAndSelection` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestPersistenceRecoversPendingAndFailedMessages` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestPersistenceRejectsCorruptionWithoutOverwrite` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestPersistenceWriteFailureBlocksProviderAndFurtherMutations` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestEachChatHasSeparateFileAndOnlyChangedFilesAreWritten` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestLegacyHistoryMigratesWithoutLosingContext` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestMissingOrCorruptChatBlocksStartupWithoutOverwritingIndex` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `persistence_test.go` | `TestOrphanChatIsRemovedWithoutResurrectingDeletedDialog` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `store.go` | `NewStore` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.SetAttemptObserver` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Close` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Create` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.CreateWithStrategy` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.List` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Get` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Exists` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Select` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.UpdateStrategy` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.AddBranch` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.SelectBranch` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Delete` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Send` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.runTitle` | workspace/conversation/taskflow + statejson |
| `store.go` | `titleErrorCategory` | workspace/conversation/taskflow + statejson |
| `store.go` | `normalizeTitle` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Retry` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.runAttempt` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.attemptConversation` | workspace/conversation/taskflow + statejson |
| `store.go` | `recordFactsUsage` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.extractFacts` | workspace/conversation/taskflow + statejson |
| `store.go` | `parseFacts` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.dialogLocked` | workspace/conversation/taskflow + statejson |
| `store.go` | `currentAgent` | workspace/conversation/taskflow + statejson |
| `store.go` | `stored *storedDialog.dialogFactsConfig` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.dialogCopyLocked` | workspace/conversation/taskflow + statejson |
| `store.go` | `branchOrder` | workspace/conversation/taskflow + statejson |
| `store.go` | `branchAccountedTokens` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.getOrCreateLocked` | workspace/conversation/taskflow + statejson |
| `store.go` | `cloneDialog` | workspace/conversation/taskflow + statejson |
| `store.go` | `cloneFacts` | workspace/conversation/taskflow + statejson |
| `store.go` | `titleFrom` | workspace/conversation/taskflow + statejson |
| `store.go` | `randomID` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.SetCompression` | workspace/conversation/taskflow + statejson |
| `store.go` | `s *Store.Compact` | workspace/conversation/taskflow + statejson |
| `store_test.go` | `p *fakeProvider.Complete` | workspace/conversation/taskflow + statejson |
| `store_test.go` | `testSnapshot` | workspace/conversation/taskflow + statejson |
| `store_test.go` | `TestStoreBuildsHistoryAndDeduplicatesClientID` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `store_test.go` | `TestStoreRetryKeepsOneUserMessage` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `store_test.go` | `TestDeletePendingDialogReleasesSessionLockAndIgnoresLateAnswer` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `store_test.go` | `TestManualCompactPreservesTranscriptAndSessionIsolation` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `title_test.go` | `p *titleProvider.Complete` | workspace/conversation/taskflow + statejson |
| `title_test.go` | `titleSnapshot` | workspace/conversation/taskflow + statejson |
| `title_test.go` | `TestSlowTitleDoesNotBlockChatOrSecondMessage` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `title_test.go` | `TestTitleGeneratedOnceAndFailureKeepsFallback` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `title_test.go` | `TestDeleteAndCloseCancelTitleWithoutResurrection` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `title_test.go` | `TestTitleTimeoutReportsFinishWithoutCancellation` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `tokens_test.go` | `TestTokenAccountingRetryRestartAndContext` | Сохранить тест; исторический gate, если обращается к /api/dialogs |

### Agent

| Файл | Объявление | Решение / эквивалент |
| --- | --- | --- |
| `agent.go` | `e *AttemptError.Error` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `e *AttemptError.Unwrap` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `s ContextStrategy.Valid` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `s DialogSnapshot.Validate` | Сохранить: active snapshots/provider и соответствующие проверки |
| `agent.go` | `s Snapshot.Validate` | Сохранить: active snapshots/provider и соответствующие проверки |
| `agent.go` | `NewConversation` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.ConfigureContext` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Strategy` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.SetBranchSuffix` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `RestoreConversation` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `RestoreCompactedConversation` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `messageNumber` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Snapshot` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Messages` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.FindClientMessage` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Begin` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Retry` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Context` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.ContextWithFacts` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Finish` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.finishCompletion` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.PruneContextMemory` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.Attempt` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.AttemptWithFacts` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `errorCategory` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.AccountedTokens` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `c *Conversation.accountedTokensLocked` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `AccountedTokens` | completion ports + openai; legacy Conversation только для session tests |
| `agent.go` | `CloneMessages` | completion ports + openai; legacy Conversation только для session tests |
| `compression_test.go` | `p *compressionProvider.Complete` | Сохранить: active snapshots/provider и соответствующие проверки |
| `compression_test.go` | `p *compressionProvider.CompleteWithUsage` | Сохранить: active snapshots/provider и соответствующие проверки |
| `compression_test.go` | `compressionConversation` | completion ports + openai; legacy Conversation только для session tests |
| `compression_test.go` | `sendCompression` | completion ports + openai; legacy Conversation только для session tests |
| `compression_test.go` | `TestCompressionBoundaryToggleAndRetry` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `compression_test.go` | `TestSummaryFailureRetainsHistory` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `compression_test.go` | `TestManualCompactIgnoresToggleAndBatchWithoutChatCall` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `compression_test.go` | `TestSummaryTranscriptIsDataIncludingUnansweredLastUser` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `openai_test.go` | `TestOpenAIProviderMapsInvalidResponse` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
| `usage_test.go` | `TestUsageSnapshotAndInterruptedAttemptRestoration` | Сохранить тест; исторический gate, если обращается к /api/dialogs |
