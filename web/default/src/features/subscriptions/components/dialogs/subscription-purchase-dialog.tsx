/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState, useEffect, useCallback, useRef } from 'react'
import { Crown, CalendarClock, Package, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SiAlipay } from 'react-icons/si'
import { toast } from 'sonner'
import { DEFAULT_CURRENCY_CONFIG } from '@/stores/system-config-store'
import { formatQuota } from '@/lib/format'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Dialog } from '@/components/dialog'
import { GroupBadge } from '@/components/group-badge'
import { AlipayF2FDialog } from '@/features/wallet/components/dialogs/alipay-f2f-dialog'
import type { AlipayF2FOrderData } from '@/features/wallet/types'
import {
  paySubscriptionStripe,
  paySubscriptionCreem,
  paySubscriptionEpay,
  paySubscriptionWaffoPancake,
  paySubscriptionAlipayF2F,
  getSubscriptionAlipayF2FOrderStatus,
  paySubscriptionBalance,
} from '../../api'
import { formatDuration, formatResetPeriod } from '../../lib'
import type { PlanRecord } from '../../types'

interface PaymentMethod {
  type: string
  name?: string
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  plan: PlanRecord | null
  enableStripe?: boolean
  enableCreem?: boolean
  enableWaffoPancake?: boolean
  enableAlipayF2F?: boolean
  alipayF2FMethod?: PaymentMethod | null
  enableOnlineTopUp?: boolean
  epayMethods?: PaymentMethod[]
  purchaseLimit?: number
  purchaseCount?: number
  userQuota?: number
  onPurchaseSuccess?: () => void | Promise<void>
}

export function SubscriptionPurchaseDialog(props: Props) {
  const { t } = useTranslation()
  const { currency } = useSystemConfig()
  const [paying, setPaying] = useState(false)
  const [selectedEpayMethod, setSelectedEpayMethod] = useState('')
  const [alipayF2FOpen, setAlipayF2FOpen] = useState(false)
  const [alipayF2FOrder, setAlipayF2FOrder] =
    useState<AlipayF2FOrderData | null>(null)
  const alipayF2FPollRef = useRef<number | null>(null)
  const alipayF2FSuccessHandledRef = useRef(false)
  const onOpenChange = props.onOpenChange
  const onPurchaseSuccess = props.onPurchaseSuccess

  const clearAlipayF2FPolling = useCallback(() => {
    if (alipayF2FPollRef.current !== null) {
      window.clearInterval(alipayF2FPollRef.current)
      alipayF2FPollRef.current = null
    }
  }, [])

  useEffect(() => {
    if (props.open && props.epayMethods && props.epayMethods.length > 0) {
      setSelectedEpayMethod(props.epayMethods[0].type)
    } else if (!props.open) {
      setSelectedEpayMethod('')
    }
  }, [props.open, props.epayMethods])

  useEffect(() => clearAlipayF2FPolling, [clearAlipayF2FPolling])

  const refreshAlipayF2FStatus = useCallback(
    async (tradeNo: string) => {
      try {
        const res = await getSubscriptionAlipayF2FOrderStatus(tradeNo)
        if (!res.success || !res.data) {
          return
        }

        const data = res.data
        setAlipayF2FOrder((prev) =>
          prev
            ? {
                ...prev,
                status: data.status || prev.status,
                trade_status: data.trade_status || prev.trade_status,
              }
            : null
        )

        if (data.status === 'success') {
          clearAlipayF2FPolling()
          if (!alipayF2FSuccessHandledRef.current) {
            alipayF2FSuccessHandledRef.current = true
            toast.success(t('Payment completed'))
            setAlipayF2FOpen(false)
            await onPurchaseSuccess?.()
          }
        } else if (data.status === 'expired') {
          clearAlipayF2FPolling()
          toast.info(t('Order expired, please initiate payment again.'))
        } else if (data.status === 'failed') {
          clearAlipayF2FPolling()
          toast.error(t('Payment failed, please try again later.'))
        }
      } catch {
        // Keep polling; short network hiccups should not close the QR dialog.
      }
    },
    [clearAlipayF2FPolling, onPurchaseSuccess, t]
  )

  const startAlipayF2FPolling = useCallback(
    (tradeNo: string) => {
      clearAlipayF2FPolling()
      alipayF2FSuccessHandledRef.current = false
      void refreshAlipayF2FStatus(tradeNo)
      alipayF2FPollRef.current = window.setInterval(() => {
        void refreshAlipayF2FStatus(tradeNo)
      }, 3000)
    },
    [clearAlipayF2FPolling, refreshAlipayF2FStatus]
  )

  const plan = props.plan?.plan
  if (!plan) return null

  const hasStripe = props.enableStripe && !!plan.stripe_price_id
  const hasCreem = props.enableCreem && !!plan.creem_product_id
  const hasWaffoPancake =
    props.enableWaffoPancake && !!plan.waffo_pancake_product_id
  const hasAlipayF2F = props.enableAlipayF2F && !!props.alipayF2FMethod
  const hasEpay =
    props.enableOnlineTopUp && (props.epayMethods || []).length > 0
  const hasAnyPayment =
    hasStripe || hasCreem || hasWaffoPancake || hasAlipayF2F || hasEpay
  const selectedEpayMethodLabel =
    (props.epayMethods || []).find((m) => m.type === selectedEpayMethod)
      ?.name ||
    selectedEpayMethod ||
    t('Select payment method')
  const totalAmount = Number(plan.total_amount || 0)
  const price = Number(plan.price_amount || 0).toFixed(2)
  const availableGroups =
    plan.available_groups && plan.available_groups.length > 0
      ? plan.available_groups
      : plan.upgrade_group
        ? [plan.upgrade_group]
        : []
  const quotaPerUnit =
    currency?.quotaPerUnit && currency.quotaPerUnit > 0
      ? currency.quotaPerUnit
      : DEFAULT_CURRENCY_CONFIG.quotaPerUnit
  const balanceCost = Math.max(
    0,
    Math.ceil(Number(plan.price_amount || 0) * quotaPerUnit)
  )
  const userQuota = Math.max(0, Number(props.userQuota || 0))
  const allowBalancePay = plan.allow_balance_pay !== false
  const insufficientBalance = userQuota < balanceCost
  const limitReached =
    (props.purchaseLimit || 0) > 0 &&
    (props.purchaseCount || 0) >= (props.purchaseLimit || 0)

  const handlePayStripe = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionStripe({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.pay_link) {
        window.open(res.data.pay_link, '_blank')
        toast.success(t('Payment page opened'))
        onOpenChange(false)
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayCreem = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionCreem({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.checkout_url) {
        window.open(res.data.checkout_url, '_blank')
        toast.success(t('Payment page opened'))
        onOpenChange(false)
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  // In-tab redirect (not window.open) — user-gesture context is lost
  // across the await, so a popup would be blocked. Same as the wallet hook.
  const handlePayWaffoPancake = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionWaffoPancake({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.checkout_url) {
        toast.success(t('Redirecting to payment page...'))
        window.location.href = res.data.checkout_url
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const isSafari =
    typeof navigator !== 'undefined' &&
    /^((?!chrome|android).)*safari/i.test(navigator.userAgent)

  const handlePayEpay = async () => {
    if (!selectedEpayMethod) {
      toast.error(t('Please select a payment method'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionEpay({
        plan_id: plan.id,
        payment_method: selectedEpayMethod,
      })
      if (res.message === 'success' && res.url) {
        const form = document.createElement('form')
        form.action = res.url
        form.method = 'POST'
        if (!isSafari) {
          form.target = '_blank'
        }
        Object.entries(res.data || {}).forEach(([key, value]) => {
          const input = document.createElement('input')
          input.type = 'hidden'
          input.name = key
          input.value = String(value)
          form.appendChild(input)
        })
        document.body.appendChild(form)
        form.submit()
        document.body.removeChild(form)
        toast.success(t('Payment initiated'))
        onOpenChange(false)
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayAlipayF2F = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionAlipayF2F({
        plan_id: plan.id,
        payment_method: 'alipay_f2f',
      })
      if (res.success && res.data?.trade_no && res.data?.qr_code) {
        const order: AlipayF2FOrderData = {
          trade_no: res.data.trade_no,
          qr_code: res.data.qr_code,
          status: res.data.status || 'pending',
          pay_money: res.data.pay_money,
          timeout_express: res.data.timeout_express,
          expires_in_sec: res.data.expires_in_sec,
          trade_status: res.data.trade_status,
        }
        setAlipayF2FOrder(order)
        setAlipayF2FOpen(true)
        onOpenChange(false)
        startAlipayF2FPolling(order.trade_no)
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayBalance = async () => {
    if (!allowBalancePay) {
      toast.error(t('This plan does not allow balance redemption'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionBalance({ plan_id: plan.id })
      if (res.success) {
        toast.success(t('Subscription purchased successfully'))
        void onPurchaseSuccess?.()
        onOpenChange(false)
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={onOpenChange}
        title={
          <>
            <Crown className='h-5 w-5' />
            {t('Purchase Subscription')}
          </>
        }
        contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'
        titleClassName='flex items-center gap-2'
        contentHeight='auto'
        bodyClassName='space-y-4'
      >
        <div className='space-y-3 sm:space-y-4'>
          <div className='bg-muted/50 space-y-2.5 rounded-lg border p-3 sm:space-y-3 sm:p-4'>
            <div className='flex justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Plan Name')}
              </span>
            <span className='max-w-[200px] truncate text-sm font-medium'>
              {plan.title}
            </span>
          </div>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Validity Period')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <CalendarClock className='h-3.5 w-3.5' />
                {formatDuration(plan, t)}
              </span>
            </div>
            {formatResetPeriod(plan, t) !== t('No Reset') && (
              <div className='flex justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Reset Period')}
                </span>
                <span className='text-sm'>{formatResetPeriod(plan, t)}</span>
              </div>
            )}
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Received amount')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <Package className='h-3.5 w-3.5' />
                {totalAmount > 0 ? formatQuota(totalAmount) : t('Unlimited')}
              </span>
            </div>
            {availableGroups.length > 0 && (
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Available Groups')}
                </span>
                <div className='flex flex-wrap justify-end gap-1'>
                  {availableGroups.map((group) => (
                    <GroupBadge key={group} group={group} />
                  ))}
                </div>
              </div>
            )}
            <Separator />
            <div className='flex items-center justify-between'>
              <span className='text-sm font-medium'>{t('Amount Due')}</span>
              <span className='text-primary text-lg font-bold'>${price}</span>
            </div>
          </div>

          {limitReached && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Purchase limit reached')} ({props.purchaseCount}/
                {props.purchaseLimit})
              </AlertDescription>
            </Alert>
          )}

          <div className='flex flex-col gap-2 rounded-md border p-3'>
            <div className='flex items-center justify-between gap-2 text-xs'>
              <span className='text-muted-foreground'>{t('Required')}</span>
              <span>{formatQuota(balanceCost)}</span>
            </div>
            <div className='flex items-center justify-between gap-2 text-xs'>
              <span className='text-muted-foreground'>{t('Available')}</span>
              <span>{formatQuota(userQuota)}</span>
            </div>
            {!allowBalancePay ? (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t('This plan does not allow balance redemption')}
                </AlertDescription>
              </Alert>
            ) : (
              insufficientBalance && (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {t('Insufficient balance')}
                  </AlertDescription>
                </Alert>
              )
            )}
            <Button
              variant='outline'
              onClick={handlePayBalance}
              disabled={
                paying ||
                limitReached ||
                !allowBalancePay ||
                insufficientBalance
              }
            >
              {t('Pay with Balance')}
            </Button>
          </div>

          {hasAnyPayment && (
            <div className='space-y-3'>
              <p className='text-muted-foreground text-xs'>
                {t('Select payment method')}
              </p>
              {(hasStripe || hasCreem || hasWaffoPancake || hasAlipayF2F) && (
                <div className='grid grid-cols-2 gap-2 sm:flex'>
                  {hasAlipayF2F && (
                    <Button
                      variant='outline'
                      className='flex-1 gap-2'
                      onClick={handlePayAlipayF2F}
                      disabled={paying || limitReached}
                    >
                      {paying ? (
                        <Loader2 className='h-4 w-4 animate-spin' />
                      ) : (
                        <SiAlipay className='h-4 w-4 text-[#1677FF]' />
                      )}
                      {props.alipayF2FMethod?.name || t('Alipay Face-to-Face')}
                    </Button>
                  )}
                  {hasStripe && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayStripe}
                      disabled={paying || limitReached}
                    >
                      Stripe
                    </Button>
                  )}
                  {hasCreem && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayCreem}
                      disabled={paying || limitReached}
                    >
                      Creem
                    </Button>
                  )}
                  {hasWaffoPancake && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayWaffoPancake}
                      disabled={paying || limitReached}
                    >
                      Waffo Pancake
                    </Button>
                  )}
                </div>
              )}
              {hasEpay && (
                <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
                  <Select
                    items={[
                      ...(props.epayMethods || []).map((m) => ({
                        value: m.type,
                        label: m.name || m.type,
                      })),
                    ]}
                    value={selectedEpayMethod}
                    onValueChange={(v) =>
                      v !== null && setSelectedEpayMethod(v)
                    }
                    disabled={limitReached}
                  >
                    <SelectTrigger className='flex-1'>
                      <SelectValue>{selectedEpayMethodLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {(props.epayMethods || []).map((m) => (
                          <SelectItem key={m.type} value={m.type}>
                            {m.name || m.type}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <Button
                    onClick={handlePayEpay}
                    disabled={paying || !selectedEpayMethod || limitReached}
                  >
                    {t('Pay')}
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </Dialog>

      <AlipayF2FDialog
        open={alipayF2FOpen}
        onOpenChange={(open) => {
          setAlipayF2FOpen(open)
          if (!open) {
            clearAlipayF2FPolling()
          }
        }}
        order={alipayF2FOrder}
        title={t('Alipay Face-to-Face')}
        instruction={t('Scan with Alipay to complete payment')}
        completionText={t(
          'The subscription will be activated automatically after payment.'
        )}
      />
    </>
  )
}
