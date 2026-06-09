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
import { Copy, Loader2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { SiAlipay } from 'react-icons/si'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import type { AlipayF2FOrderData } from '../../types'

interface AlipayF2FDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  order: AlipayF2FOrderData | null
  title?: string
  instruction?: string
  completionText?: string
}

function getStatusLabel(
  status: string | undefined,
  t: (key: string) => string
) {
  switch (status) {
    case 'success':
      return t('Paid')
    case 'expired':
      return t('Expired')
    case 'failed':
      return t('Payment Failed')
    default:
      return t('Waiting for payment')
  }
}

function getStatusVariant(status: string | undefined) {
  switch (status) {
    case 'success':
      return 'success'
    case 'expired':
    case 'failed':
      return 'danger'
    default:
      return 'info'
  }
}

export function AlipayF2FDialog({
  open,
  onOpenChange,
  order,
  title,
  instruction,
  completionText,
}: AlipayF2FDialogProps) {
  const { t } = useTranslation()
  const status = order?.status || 'pending'
  const isPending = status === 'pending'

  const copyTradeNo = async () => {
    if (!order?.trade_no) return
    try {
      await navigator.clipboard.writeText(order.trade_no)
      toast.success(t('Copied'))
    } catch {
      toast.error(t('Copy failed'))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        <span className='flex items-center gap-2'>
          <SiAlipay className='h-5 w-5 text-[#1677FF]' />
          {title || t('Alipay Face-to-Face')}
        </span>
      }
      description={instruction || t('Scan with Alipay to complete payment')}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-sm'
      contentHeight='auto'
      bodyClassName='space-y-4'
      showCloseButton={!isPending}
    >
      <div className='flex flex-col items-center gap-4'>
        {order?.qr_code ? (
          <div className='rounded-lg border bg-white p-4 shadow-sm'>
            <QRCodeSVG value={order.qr_code} size={220} />
          </div>
        ) : (
          <div className='bg-muted flex h-[252px] w-[252px] items-center justify-center rounded-lg border'>
            <Loader2 className='text-muted-foreground h-6 w-6 animate-spin' />
          </div>
        )}

        <StatusBadge
          variant={getStatusVariant(status)}
          label={getStatusLabel(status, t)}
          copyable={false}
        />

        {order?.pay_money ? (
          <div className='flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm'>
            <span className='text-muted-foreground'>
              {t('Actual paid amount')}
            </span>
            <span className='font-medium'>¥{order.pay_money}</span>
          </div>
        ) : null}

        {order?.trade_no ? (
          <div className='grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded-md border px-3 py-2 text-sm'>
            <div className='min-w-0'>
              <div className='text-muted-foreground text-xs'>
                {t('Order No.')}
              </div>
              <div className='truncate font-mono text-xs'>{order.trade_no}</div>
            </div>
            <Button
              variant='ghost'
              size='icon'
              className='h-8 w-8'
              onClick={copyTradeNo}
            >
              <Copy className='h-4 w-4' />
            </Button>
          </div>
        ) : null}

        {order?.trade_status ? (
          <div className='text-muted-foreground text-center text-xs'>
            {t('Payment Status')}: {order.trade_status}
          </div>
        ) : null}

        <p className='text-muted-foreground text-center text-xs'>
          {completionText ||
            t('The order will refresh automatically after payment.')}
        </p>
      </div>
    </Dialog>
  )
}
