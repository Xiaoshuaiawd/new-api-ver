/*
Copyright (C) 2025 QuantumNous

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

import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
  toBoolean,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { BookOpen, TriangleAlert } from 'lucide-react';

export default function SettingsPaymentGatewayAlipayF2F(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle
    ? undefined
    : t('支付宝当面付设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    AlipayF2FEnabled: false,
    AlipayF2FAppID: '',
    AlipayF2FSellerID: '',
    AlipayF2FPrivateKey: '',
    AlipayF2FPublicKey: '',
    AlipayF2FGateway: 'https://openapi.alipay.com/gateway.do',
    AlipayF2FDisplayName: '支付宝当面付',
    AlipayF2FMinTopUp: 1,
    AlipayF2FOrderTimeout: 30,
    AlipayF2FSubjectPrefix: 'new-api',
  });
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        AlipayF2FEnabled: toBoolean(props.options.AlipayF2FEnabled),
        AlipayF2FAppID: props.options.AlipayF2FAppID || '',
        AlipayF2FSellerID: props.options.AlipayF2FSellerID || '',
        AlipayF2FPrivateKey: '',
        AlipayF2FPublicKey: props.options.AlipayF2FPublicKey || '',
        AlipayF2FGateway:
          props.options.AlipayF2FGateway ||
          'https://openapi.alipay.com/gateway.do',
        AlipayF2FDisplayName:
          props.options.AlipayF2FDisplayName || '支付宝当面付',
        AlipayF2FMinTopUp:
          props.options.AlipayF2FMinTopUp !== undefined
            ? parseFloat(props.options.AlipayF2FMinTopUp)
            : 1,
        AlipayF2FOrderTimeout:
          props.options.AlipayF2FOrderTimeout !== undefined
            ? parseFloat(props.options.AlipayF2FOrderTimeout)
            : 30,
        AlipayF2FSubjectPrefix:
          props.options.AlipayF2FSubjectPrefix || 'new-api',
      };
      setInputs(currentInputs);
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitAlipayF2FSetting = async () => {
    if (props.options.ServerAddress === '') {
      showError(t('请先填写服务器地址'));
      return;
    }

    setLoading(true);
    try {
      const options = [
        {
          key: 'AlipayF2FEnabled',
          value: inputs.AlipayF2FEnabled ? 'true' : 'false',
        },
        {
          key: 'AlipayF2FAppID',
          value: inputs.AlipayF2FAppID || '',
        },
        {
          key: 'AlipayF2FSellerID',
          value: inputs.AlipayF2FSellerID || '',
        },
        {
          key: 'AlipayF2FPublicKey',
          value: inputs.AlipayF2FPublicKey || '',
        },
        {
          key: 'AlipayF2FGateway',
          value: removeTrailingSlash(inputs.AlipayF2FGateway || ''),
        },
        {
          key: 'AlipayF2FDisplayName',
          value: inputs.AlipayF2FDisplayName || '支付宝当面付',
        },
        {
          key: 'AlipayF2FMinTopUp',
          value: String(inputs.AlipayF2FMinTopUp || 1),
        },
        {
          key: 'AlipayF2FOrderTimeout',
          value: String(inputs.AlipayF2FOrderTimeout || 30),
        },
        {
          key: 'AlipayF2FSubjectPrefix',
          value: inputs.AlipayF2FSubjectPrefix || 'new-api',
        },
      ];

      if (
        inputs.AlipayF2FPrivateKey &&
        inputs.AlipayF2FPrivateKey.trim() !== ''
      ) {
        options.push({
          key: 'AlipayF2FPrivateKey',
          value: inputs.AlipayF2FPrivateKey,
        });
      }

      const results = await Promise.all(
        options.map((option) =>
          API.put('/api/option/', {
            key: option.key,
            value: option.value,
          }),
        ),
      );

      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  const callbackBase =
    props.options.CustomCallbackAddress && props.options.CustomCallbackAddress
      ? removeTrailingSlash(props.options.CustomCallbackAddress)
      : props.options.ServerAddress
        ? removeTrailingSlash(props.options.ServerAddress)
        : '';

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<BookOpen size={16} />}
            description={
              <>
                {t(
                  '请在支付宝开放平台创建当面付应用，并配置应用公钥、支付宝公钥和异步通知地址。',
                )}
                <br />
                {t('异步通知地址')}：{callbackBase || t('请先配置服务器地址')}
                /api/alipay-f2f/notify
              </>
            }
            style={{ marginBottom: 12 }}
          />
          <Banner
            type='warning'
            icon={<TriangleAlert size={16} />}
            description={t(
              '商户应用私钥保存后不会回显；如无需更新，请留空提交。',
            )}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='AlipayF2FEnabled'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('启用支付宝当面付')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayF2FAppID'
                label={t('应用 AppID')}
                placeholder={t('例如：2026xxxxxxxxxxxx')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayF2FSellerID'
                label={t('商户 PID / Seller ID（可选）')}
                placeholder={t('例如：2088xxxxxxxxxxxx，可留空')}
                extraText={t(
                  '当前版本可留空；如后续你拿到 PID，再补上可额外增强回调校验。',
                )}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayF2FDisplayName'
                label={t('前台显示名称')}
                placeholder={t('例如：支付宝扫码 / 扫码支付')}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='AlipayF2FPrivateKey'
                label={t('应用私钥')}
                placeholder={t(
                  '请粘贴 RSA/RSA2 应用私钥，保存后不会回显；留空表示保持当前不变',
                )}
                autosize
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='AlipayF2FPublicKey'
                label={t('支付宝公钥')}
                placeholder={t('请粘贴支付宝开放平台中的支付宝公钥')}
                autosize
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayF2FGateway'
                label={t('网关地址')}
                placeholder='https://openapi.alipay.com/gateway.do'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='AlipayF2FMinTopUp'
                label={t('最低充值数量')}
                min={1}
                precision={0}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='AlipayF2FOrderTimeout'
                label={t('订单超时（分钟）')}
                min={1}
                precision={0}
              />
            </Col>
          </Row>
          <Row style={{ marginTop: 16 }}>
            <Col span={24}>
              <Form.Input
                field='AlipayF2FSubjectPrefix'
                label={t('订单标题前缀')}
                placeholder={t('例如：new-api')}
                extraText={t('将作为支付宝订单标题的前缀展示')}
              />
            </Col>
          </Row>
          <Button onClick={submitAlipayF2FSetting} style={{ marginTop: 16 }}>
            {t('更新支付宝当面付设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
