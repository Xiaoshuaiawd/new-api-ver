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
import { useCallback, useEffect, useRef, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  getAlipayF2FTopupStatus,
  isApiSuccess,
  requestAlipayF2FPayment,
} from '../api'
import { PAYMENT_TYPES } from '../constants'
import type { AlipayF2FOrderData } from '../types'

const POLL_INTERVAL_MS = 3000

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }

  return message || i18next.t('Payment request failed')
}

export function useAlipayF2FPayment(
  onPaymentSuccess?: () => void | Promise<void>
) {
  const [open, setOpen] = useState(false)
  const [order, setOrder] = useState<AlipayF2FOrderData | null>(null)
  const [processing, setProcessing] = useState(false)
  const pollRef = useRef<number | null>(null)
  const successHandledRef = useRef(false)

  const clearPolling = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [])

  const refreshStatus = useCallback(
    async (tradeNo: string) => {
      try {
        const response = await getAlipayF2FTopupStatus(tradeNo)
        if (!isApiSuccess(response) || !response.data) {
          return
        }

        const data = response.data
        setOrder((previous) =>
          previous
            ? {
                ...previous,
                status: data.status || previous.status,
                trade_status: data.trade_status || previous.trade_status,
              }
            : null
        )

        if (data.status === 'success') {
          clearPolling()
          if (!successHandledRef.current) {
            successHandledRef.current = true
            toast.success(i18next.t('Payment completed'))
            await onPaymentSuccess?.()
          }
        } else if (data.status === 'expired') {
          clearPolling()
          toast.info(i18next.t('Order expired, please initiate payment again.'))
        } else if (data.status === 'failed') {
          clearPolling()
          toast.error(i18next.t('Payment failed, please try again later.'))
        }
      } catch {
        // Keep polling; short network hiccups should not close the QR dialog.
      }
    },
    [clearPolling, onPaymentSuccess]
  )

  const startPolling = useCallback(
    (tradeNo: string) => {
      clearPolling()
      successHandledRef.current = false
      void refreshStatus(tradeNo)
      pollRef.current = window.setInterval(() => {
        void refreshStatus(tradeNo)
      }, POLL_INTERVAL_MS)
    },
    [clearPolling, refreshStatus]
  )

  const processAlipayF2FPayment = useCallback(
    async (topupAmount: number) => {
      setProcessing(true)

      try {
        const response = await requestAlipayF2FPayment({
          amount: Math.floor(topupAmount),
          payment_method: PAYMENT_TYPES.ALIPAY_F2F,
        })

        if (isApiSuccess(response) && response.data) {
          setOrder(response.data)
          setOpen(true)
          if (response.data.trade_no) {
            startPolling(response.data.trade_no)
          }
          return true
        }

        toast.error(getErrorMessage(response.message, response.data))
        return false
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    [startPolling]
  )

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen)
      if (!nextOpen) {
        clearPolling()
      }
    },
    [clearPolling]
  )

  useEffect(() => clearPolling, [clearPolling])

  return {
    alipayF2FOpen: open,
    alipayF2FOrder: order,
    alipayF2FProcessing: processing,
    setAlipayF2FOpen: handleOpenChange,
    processAlipayF2FPayment,
  }
}
