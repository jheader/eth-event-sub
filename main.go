package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// 1. 定义 ERC-20 Transfer 事件的 ABI（仅保留核心事件）
const transferABI = `[
	{
		"anonymous": false,
		"inputs": [
			{"indexed": true, "name": "from", "type": "address"},
			{"indexed": true, "name": "to", "type": "address"},
			{"indexed": false, "name": "value", "type": "uint256"}
		],
		"name": "Transfer",
		"type": "event"
	}
]`

func main() {
	// ========== 步骤1：解析命令行参数（指定合约地址） ==========
	contractAddrStr := flag.String("contract", "0xdAC17F958D2ee523a2206206994597C13D831ec7", "ERC-20 contract address")
	flag.Parse()
	contractAddr := common.HexToAddress(*contractAddrStr) // 转为以太坊地址类型

	// ========== 步骤2：获取节点地址（优先 WSS） ==========
	// 需提前设置环境变量：export ETH_WS_URL="wss://mainnet.infura.io/ws/v3/你的API_KEY"
	rpcURL := os.Getenv("ETH_WS_URL")
	if rpcURL == "" {
		log.Fatal("请设置 ETH_WS_URL 环境变量（WebSocket 地址）")
	}

	// ========== 步骤3：创建上下文（用于优雅退出） ==========
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 函数退出时取消上下文

	// ========== 步骤4：连接以太坊节点 ==========
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		log.Fatalf("连接节点失败：%v", err)
	}
	defer client.Close() // 函数退出时关闭连接
	fmt.Printf("成功连接节点：%s\n", rpcURL)

	// ========== 步骤5：解析 ABI（用于解码事件） ==========
	parsedABI, err := abi.JSON(strings.NewReader(transferABI))
	if err != nil {
		log.Fatalf("解析 ABI 失败：%v", err)
	}

	// ========== 步骤6：定义日志过滤规则 ==========
	// FilterQuery 是订阅的核心：指定要监听的合约、事件等
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddr}, // 仅监听指定合约的日志
		// 可选：指定 Topics 过滤特定事件（比如只监听 from 为某地址的 Transfer）
		// Topics: [][]common.Hash{
		// 	{common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")}, // Transfer 事件签名哈希
		// 	{common.HexToHash("0x000000000000000000000000你的地址000000000000000000000000000000")}, // from 地址（indexed）
		// },
	}

	// ========== 步骤7：创建日志订阅 ==========
	logsCh := make(chan types.Log) // 接收日志的通道
	sub, err := client.SubscribeFilterLogs(ctx, query, logsCh)
	if err != nil {
		log.Fatalf("创建订阅失败：%v", err)
	}
	fmt.Printf("成功订阅合约 %s 的 Transfer 事件，监听中...\n", contractAddr.Hex())

	// ========== 步骤8：监听系统信号（Ctrl+C 优雅退出） ==========
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// ========== 步骤9：主循环（监听日志/错误/信号） ==========
	for {
		select {
		// 收到新日志
		case log := <-logsCh:
			parseTransferEvent(log, parsedABI) // 解析并打印 Transfer 事件

		// 订阅出错（比如节点断开）
		case err := <-sub.Err():
			log.Printf("订阅出错：%v，退出监听", err)
			return

		// 收到退出信号（Ctrl+C）
		case sig := <-sigCh:
			fmt.Printf("\n收到信号 %s，优雅退出...\n", sig)
			return

		// 上下文被取消
		case <-ctx.Done():
			fmt.Println("上下文取消，退出监听")
			return
		}
	}
}

// parseTransferEvent 解析 Transfer 事件日志，输出可读参数
func parseTransferEvent(log types.Log, parsedABI abi.ABI) {
	// 定义结构体接收解码后的参数（需与 ABI 对应）
	type TransferEvent struct {
		From  common.Address
		To    common.Address
		Value *big.Int
	}
	var event TransferEvent

	// 解码日志：将 Data + Topics 转为结构体
	err := parsedABI.UnpackIntoInterface(&event, "Transfer", log.Data)
	if err != nil {
		fmt.Printf("解码 Data 失败：%v", err)
		return
	}

	// 手动解析 indexed 参数（From/To 存在 Topics 中）
	// Topics[0] = 事件签名哈希，Topics[1] = From，Topics[2] = To
	event.From = common.BytesToAddress(log.Topics[1].Bytes()[12:]) // 地址仅占后20字节
	event.To = common.BytesToAddress(log.Topics[2].Bytes()[12:])

	// 打印结构化事件
	fmt.Printf("=====================================\n")
	fmt.Printf("时间：%s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("区块号：%d\n", log.BlockNumber)
	fmt.Printf("交易哈希：%s\n", log.TxHash.Hex())
	fmt.Printf("Transfer 事件：\n")
	fmt.Printf("  从：%s\n", event.From.Hex())
	fmt.Printf("  到：%s\n", event.To.Hex())
	fmt.Printf("  金额：%s\n", event.Value.String()) // USDT 是 6 位小数，需除以 1e6
	fmt.Printf("=====================================\n\n")
}
