# task26100535

云资源多级配额核账库（Go），由上层直接调用，状态保存在进程内存。

## 功能

统一约束租户、部门、个人三个层级的四类额度：在用单位数、并行请求数、
存储字节数、逐月预算（整数分）。支持四类操作：

- `Approve(ApproveRequest)` 批准请求：同时检查三级四类上限，所有覆盖月份
  均不得超额；任一不满足则整体不占额。重复批准幂等，已关闭编号不可重建。
- `Revise(Revision)` 接收计费修订：较大修订号整体替换旧快照，旧号重放无效；
  实账即使超额也入账。
- `Close(id)` 关闭请求：资源占用归零，各月预算只保留实账；可重复调用，
  关闭后仍接受修订。
- `Query(user, month)` 查询：返回用户/部门/租户三级在同一一致时刻的用量、
  实账、未结承诺与告警状态（OK/WARNING/CRITICAL/BLOCKED）。

## 用法

```go
l := quotaledger.New(quotaledger.Config{...})
err := l.Approve(quotaledger.ApproveRequest{
    ID: "r1", User: "u1", Units: 2, StoragePerUnit: 100,
    MonthlyFeePerUnit: 8000, StartMonth: quotaledger.MonthOf(2026, 1),
    DurationMonths: 12,
})
res, err := l.Query("u1", quotaledger.MonthOf(2026, 1))
```

所有操作通过单把互斥锁保证整体生效与一致快照，可并发交错调用。

## 测试

```sh
go test ./...        # 全部测试（含 10 万请求混合负载性能用例）
go test -race ./...  # 竞态检测
```
