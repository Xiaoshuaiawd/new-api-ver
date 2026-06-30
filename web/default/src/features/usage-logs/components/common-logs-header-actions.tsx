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
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Loader2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { startLogBodyCleanupTask } from '../api'
import { CommonLogsStats } from './common-logs-stats'
import { useUsageLogsContext } from './usage-logs-provider'

/**
 * Page-header actions for the Common Logs view: live usage stats plus a
 * toggle for masking sensitive values (token names, usernames, group names,
 * and the quota figure shown in stats). Both controls live in the page
 * header so the toolbar below stays focused on filter inputs and form
 * actions only.
 */
export function CommonLogsHeaderActions() {
  const { t } = useTranslation()
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const queryClient = useQueryClient()
  const userRole = useAuthStore((state) => state.auth.user?.role ?? 0)
  const isRoot = userRole >= ROLE.SUPER_ADMIN
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [isStartingCleanup, setIsStartingCleanup] = useState(false)

  const handleClearBodyDetails = async () => {
    setIsStartingCleanup(true)
    try {
      const result = await startLogBodyCleanupTask()
      if (!result.success) {
        toast.error(result.message || t('Failed to start log body cleanup'))
        return
      }
      toast.success(t('Log body cleanup started in the background.'))
      setConfirmOpen(false)
      await queryClient.invalidateQueries({ queryKey: ['logs'] })
      await queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
    } catch {
      toast.error(t('Failed to start log body cleanup'))
    } finally {
      setIsStartingCleanup(false)
    }
  }

  return (
    <>
      <div className='flex flex-wrap items-center gap-2'>
        <CommonLogsStats />
        {isRoot ? (
          <Button
            variant='destructive'
            size='sm'
            className='h-7 gap-1.5 px-2 text-xs'
            onClick={() => setConfirmOpen(true)}
            disabled={isStartingCleanup}
          >
            {isStartingCleanup ? (
              <Loader2 className='size-3.5 animate-spin' />
            ) : (
              <Trash2 className='size-3.5' />
            )}
            {t('Clear bodies')}
          </Button>
        ) : null}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='ghost'
                size='icon'
                onClick={() => setSensitiveVisible(!sensitiveVisible)}
                aria-label={sensitiveVisible ? t('Hide') : t('Show')}
                className='text-muted-foreground hover:text-foreground size-7'
              />
            }
          >
            {sensitiveVisible ? <Eye /> : <EyeOff />}
          </TooltipTrigger>
          <TooltipContent>
            {sensitiveVisible ? t('Hide') : t('Show')}
          </TooltipContent>
        </Tooltip>
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Clear request and response bodies?')}
        desc={t(
          'This starts a background task to remove saved request and response bodies from log details, while keeping log rows and other diagnostic fields.'
        )}
        confirmText={
          isStartingCleanup ? t('Starting...') : t('Start cleanup')
        }
        destructive
        isLoading={isStartingCleanup}
        handleConfirm={handleClearBodyDetails}
      />
    </>
  )
}
