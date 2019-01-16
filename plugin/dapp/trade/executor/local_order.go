// Copyright Fuzamei Corp. 2018 All Rights Reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package executor

import (
	"github.com/33cn/chain33/common/db/table"
	pty "github.com/33cn/plugin/plugin/dapp/trade/types"

	"github.com/33cn/chain33/types"
	"fmt"
	"github.com/33cn/chain33/common/db"
	"github.com/33cn/chain33/common"
	"strconv"
)

/*
现有接口
 1.  查询地址对应的买单 （无分页）
   1.1 只指定地址   -> owner
   1.2 同时指定地址和token  -> owner_asset
   1.3 显示一个用户成交的所有买单 -> owner
   1.4 显示一个用户成交的指定一个或者多个token所有买单 -> owner_asset 不支持多个
 2. 分状态查询地址的买单： 状态 地址 （无分页） -> owner_status
 3. 显示一个token 指定数量的买单 GetTokenBuyOrderByStatus  -> asset_inBuy_status
 4. 显示指定token出售者的一个或多个token 或 不指定token 的卖单 （无分页） -> owner_asset/owner_asset_isSell 不支持多个
 5. 显示指定状态下的某地址卖单 （无分页）  -> owner_isSell_status
 6. 显示一个token 指定数量的卖单    -> asset_isSell
 7. 根据状态分页列出某地址的订单（包括买单卖单） owner_status

*/
var opt_order_table = &table.Option{
	Prefix: "LODB_trade",
	Name: "order",
	Primary: "txIndex",
	// asset = asset_exec+asset_symbol
	//
	// status: 设计为可以同时查询几种的并集 , 存储为前缀， 需要提前设计需要合并的， 用前缀表示
	//    进行中，  撤销，  部分成交 ， 全部成交，  完成状态统一前缀. 数字和原来不一样
	//      00     10     11          12         1*
	// 排序过滤条件： 可以组合，status&isSell 和前缀冲突
	Index: []string{
		"key", // 内部查询用
		"asset", // 按资产统计订单
		"asset_isSell_status", // 接口 3
		// "asset_status", 可能需求， 用于资产的交易历史
		// "asset_isSell",
		"owner", // 接口 1.1， 1.3
		"owner_asset", // 接口 1.2, 1.4, 4, 7
		"owner_asset_isSell", // 接口 4
		"owner_asset_status", // 新需求， 在
		"owner_isSell",  // 接口 6
		// "owner_isSell_status",  可能需求， 界面分开显示订单
		// "owner_isSell_statusPrefix", // 状态可以定制组合, 成交历史需求
		"owner_status", // 接口 2
		// "owner_statusPrefix", // 状态可以定制组合 , 成交历史需求
	},
}


type OrderRow struct {
	*pty.LocalOrder
}

func NewOrderRew() *OrderRow {
	return &OrderRow{LocalOrder: nil}
}

func (r *OrderRow) CreateRow() *table.Row {
	return &table.Row{Data: &pty.ReplyTradeOrder{}}
}

func (r *OrderRow) SetPayload(data types.Message) error {
	if d, ok := data.(*pty.LocalOrder); ok {
		r.LocalOrder = d
		return nil
	}
	return types.ErrTypeAsset
}

// TODO more
func (r *OrderRow) Get(key string) ([]byte, error) {
	switch key {
	case "txIndex":
		return []byte(fmt.Sprintf("%s", r.TxIndex)), nil
	case "key":
		return []byte(fmt.Sprintf("%s", r.Key)), nil
	case "asset":
		return []byte(fmt.Sprintf("%s", r.asset())), nil
	case "asset_isSell_status":
		return []byte(fmt.Sprintf("%s_%d_", r.asset(), r.isSell(), r.status())), nil
	case "owner":
		return []byte(fmt.Sprintf("%s", r.Owner)), nil
	case "owner_asset":
		return []byte(fmt.Sprintf("%s_%s", r.Owner, r.asset())), nil
	case "owner_asset_isSell":
		return []byte(fmt.Sprintf("%s_%s_%d", r.Owner, r.asset(), r.isSell())), nil
	case "owner_asset_status":
		return []byte(fmt.Sprintf("%s_%s_%s", r.Owner, r.asset(), r.status())), nil
	case "owner_isSell":
		return []byte(fmt.Sprintf("%s_%d", r.Owner, r.isSell())), nil
	//case "owner_isSell_statusPrefix":
	//	return []byte(fmt.Sprintf("%s_%d_%s", r.Owner, r.asset(), r.isSell())), nil
	case "owner_status":
		return []byte(fmt.Sprintf("%s_%s", r.Owner, r.status())), nil
	//case "owner_statusPrefix":
	//	return []byte(fmt.Sprintf("%s_%d", r.Owner, r.isSell())), nil

	default:
		return nil, types.ErrNotFound
	}
}

func (r *OrderRow) asset() string {
	return r.LocalOrder.AssetExec + "." + r.LocalOrder.AssetSymbol
}

func (r *OrderRow) isSell() int {
	if r.IsSellOrder {
		return 1
	}
	return 0
}

// status: 设计为可以同时查询几种的并集 , 存储为前缀， 需要提前设计需要合并的， 用前缀表示
//    进行中，  撤销，  部分成交 ， 全部成交，  完成状态统一前缀. 数字和原来不一样
//      01     10     11          12        19 -> 1*
func (r *OrderRow) status() string {
	// if r.Status == 1 || r.Status == 10 || r.Status == 11 || r.Status == 12 {}
	if r.Status == 19 {
		return "1" // 试图用1 可以匹配所有完成的
	}
	return fmt.Sprintf("%02d", r.Status)
}

func NewOrderTable(kvdb db.KV) *table.Table {
	rowMeta := NewOrderRew()
	t, err := table.NewTable(rowMeta, kvdb, opt_order_table)
	if err != nil {
		panic(err)
	}
	return t
}

func (t *trade) genSellLimit(tx *types.Transaction, sell *pty.ReceiptSellBase,
	sellorder *pty.SellOrder, txIndex string) *pty.LocalOrder {

	status := sellorder.Status
	if status == pty.TradeOrderStatusRevoked || sell.SoldBoardlot > 0 {
		status = pty.TradeOrderStatusSellHalfRevoked
	}
	order := &pty.LocalOrder{
		AssetSymbol:          sellorder.TokenSymbol,
		Owner:                sellorder.Address,
		AmountPerBoardlot:    sellorder.AmountPerBoardlot,
		MinBoardlot:          sellorder.MinBoardlot,
		PricePerBoardlot:     sellorder.PricePerBoardlot,
		TotalBoardlot:        sellorder.TotalBoardlot,
		TradedBoardlot:       sellorder.SoldBoardlot,
		BuyID:                "",
		Status:               status,
		SellID:               sell.SellID,
		TxHash:               []string{common.ToHex(tx.Hash())},
		Height:               sell.Height,
		Key:                  sell.SellID,
		BlockTime:            t.GetBlockTime(),
		IsSellOrder:          true,
		AssetExec:            sellorder.AssetExec,
		TxIndex:              txIndex,
	}
	return order
}

func (t *trade) updateSellLimit(tx *types.Transaction, sell *pty.ReceiptSellBase,
	sellorder *pty.SellOrder, txIndex string, ldb *table.Table) *pty.LocalOrder {

	xs, err := ldb.ListIndex("key", []byte(sell.SellID), nil, 1, 0)
	if err != nil || len(xs) != 1 {
		return nil
	}
	order, ok := xs[0].Data.(*pty.LocalOrder)
	if !ok {
		return nil

	}
	status := sellorder.Status
	if status == pty.TradeOrderStatusRevoked && sell.SoldBoardlot > 0 {
		status = pty.TradeOrderStatusSellHalfRevoked
	}
	order.Status = status
	order.TxHash = append(order.TxHash, common.ToHex(tx.Hash()))
	order.TradedBoardlot = sellorder.SoldBoardlot

	ldb.Replace(order)

	return order
}

func parseOrderAmountFloat(s string) int64 {
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		tradelog.Error("parseOrderAmountFloat", "decode receipt", err)
		return 0
	}
	return int64(x * float64(types.TokenPrecision))
}

func parseOrderPriceFloat(s string) int64 {
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		tradelog.Error("parseOrderPriceFloat", "decode receipt", err)
		return 0
	}
	return int64(x * float64(types.Coin))
}


func (t *trade) genSellMarket(tx *types.Transaction, sell *pty.ReceiptSellBase, txIndex string) *pty.LocalOrder {

	order := &pty.LocalOrder{
		AssetSymbol:          sell.TokenSymbol,
		Owner:                sell.Owner,
		AmountPerBoardlot:    parseOrderAmountFloat(sell.AmountPerBoardlot),
		MinBoardlot:          sell.MinBoardlot,
		PricePerBoardlot:     parseOrderPriceFloat(sell.PricePerBoardlot),
		TotalBoardlot:        sell.TotalBoardlot,
		TradedBoardlot:       sell.SoldBoardlot,
		BuyID:                sell.BuyID,
		Status:               pty.TradeOrderStatusSoldOut,
		SellID:               calcTokenSellID(common.Bytes2Hex(tx.Hash())),
		TxHash:               []string{common.ToHex(tx.Hash())},
		Height:               sell.Height,
		Key:                  calcTokenSellID(common.Bytes2Hex(tx.Hash())),
		BlockTime:            t.GetBlockTime(),
		IsSellOrder:          true,
		AssetExec:            sell.AssetExec,
		TxIndex:              txIndex,
	}
	return order
}

func (t *trade) genBuyLimit(tx *types.Transaction, buy *pty.ReceiptBuyBase, txIndex string) *pty.LocalOrder {

	order := &pty.LocalOrder{
		AssetSymbol:       buy.TokenSymbol,
		Owner:             buy.Owner,
		AmountPerBoardlot: parseOrderAmountFloat(buy.AmountPerBoardlot),
		MinBoardlot:       buy.MinBoardlot,
		PricePerBoardlot:  parseOrderPriceFloat(buy.PricePerBoardlot),
		TotalBoardlot:     buy.TotalBoardlot,
		TradedBoardlot:    buy.BoughtBoardlot,
		BuyID:             buy.BuyID,
		Status:            pty.TradeOrderStatusOnBuy,
		SellID:            "",
		TxHash:            []string{common.ToHex(tx.Hash())},
		Height:            buy.Height,
		Key:               buy.BuyID,
		BlockTime:         t.GetBlockTime(),
		IsSellOrder:       true,
		AssetExec:         buy.AssetExec,
		TxIndex:           txIndex,
	}
	return order
}

func (t *trade) updateBuyLimit(tx *types.Transaction, buy *pty.ReceiptBuyBase,
	buyorder *pty.BuyLimitOrder, txIndex string, ldb *table.Table) *pty.LocalOrder {

	xs, err := ldb.ListIndex("key", []byte(buy.SellID), nil, 1, 0)
	if err != nil || len(xs) != 1 {
		return nil
	}
	order, ok := xs[0].Data.(*pty.LocalOrder)
	if !ok {
		return nil

	}
	status := buyorder.Status
	if status == pty.TradeOrderStatusRevoked && buy.BoughtBoardlot > 0 {
		status = pty.TradeOrderStatusSellHalfRevoked
	}
	order.Status = status
	order.TxHash = append(order.TxHash, common.ToHex(tx.Hash()))
	order.TradedBoardlot = buyorder.BoughtBoardlot

	ldb.Replace(order)

	return order
}

func (t *trade) genBuyMarket(tx *types.Transaction, buy *pty.ReceiptBuyBase, txIndex string) *pty.LocalOrder {

	order := &pty.LocalOrder{
		AssetSymbol:       buy.TokenSymbol,
		Owner:             buy.Owner,
		AmountPerBoardlot: parseOrderAmountFloat(buy.AmountPerBoardlot),
		MinBoardlot:       buy.MinBoardlot,
		PricePerBoardlot:  parseOrderPriceFloat(buy.PricePerBoardlot),
		TotalBoardlot:     buy.TotalBoardlot,
		TradedBoardlot:    buy.BoughtBoardlot,
		BuyID:             calcTokenBuyID(common.Bytes2Hex(tx.Hash())),
		Status:            pty.TradeOrderStatusBoughtOut,
		SellID:            buy.SellID,
		TxHash:            []string{common.ToHex(tx.Hash())},
		Height:            buy.Height,
		Key:               calcTokenBuyID(common.Bytes2Hex(tx.Hash())),
		BlockTime:         t.GetBlockTime(),
		IsSellOrder:       true,
		AssetExec:         buy.AssetExec,
		TxIndex:           txIndex,
	}
	return order
}

/*



按 资产 查询 ：
按 资产 & 地址 查询
按 地址

排序和分类
 1. 时间顺序   txindex
 1. 分类， 不同的状态 & 不同的性质： 买/卖

交易 -> 订单 按订单来 (交易和订单是多对多的关系，不适合joinTable)

交易 T1 Create -> T2 part-take -> T3 Revoke

订单左为进行中， 右为完成，
订单   （C1) | () ->  (C1m) | (C2) -> () | (C2, C1r)


查询交易 / 查询订单
  C ->   C/M -> C/D
  \
   \ ->R

	sellOrderSHTAS = "LODB-trade-sellorder-shtas:"   status, height, token, addr, sellOrderID
	sellOrderASTS  = "LODB-trade-sellorder-asts:"     addr, status, token, sellOrderID
	sellOrderATSS  = "LODB-trade-sellorder-atss:"    addr, token, status, sellOrderID
	sellOrderTSPAS = "LODB-trade-sellorder-tspas:"   token, status, price, addr, orderID

	buyOrderSHTAS  = "LODB-trade-buyorder-shtas:"
	buyOrderASTS   = "LODB-trade-buyorder-asts:"
	buyOrderATSS   = "LODB-trade-buyorder-atss:"
	buyOrderTSPAS  = "LODB-trade-buyorder-tspas:"

	// Addr-Status-Type-Height-Key
	orderASTHK = "LODB-trade-order-asthk:"    addr, status, height, ty, key


)
 */

/*
状态 1, TradeOrderStatusOnSale, 在售
状态 2： TradeOrderStatusSoldOut，售完
状态 3： TradeOrderStatusRevoked， 卖单被撤回
					  状态 4： TradeOrderStatusExpired， 订单超时(目前不支持订单超时)
状态 5： TradeOrderStatusOnBuy， 求购
状态 6： TradeOrderStatusBoughtOut， 购买完成
状态 7： TradeOrderStatusBuyRevoked， 买单被撤回
*/
