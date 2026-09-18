/*
 * 居民档案数据层 · 单一可信源 (Single Source of Truth)
 * -------------------------------------------------------------
 * 由原本分散的 data/residents/1.js~12.js 12 个全局脚本合并而来，
 * 字段结构经 schema.js 校验一致。新增/修改居民只需编辑本文件中的对象，
 * 无需再维护 12 个碎片文件。
 *
 * 数据结构（详见 data/schema.js）：
 *   { [id:number]: {
 *       id, name, gender('男'|'女'), age,
 *       chronic(慢性病摘要), height(cm), weight(kg),
 *       place(楼栋/区域), mobility(0~1 活动能力), careLevel('自理'|'介护'...),
 *       heartRate, bloodOxygen, temperature, systolic, diastolic, bloodSugar (当前快照),
 *       medicalHistory:{ pastHistory, allergies, medications, surgicalHistory,
 *                        familyHistory, smoking, alcohol, bloodType, lastCheckup },
 *       slots:[ {name:'清晨'|'上午'|'中午'|'下午'|'晚间', heartRate, bloodOxygen,
 *                temperature, systolic, diastolic, bloodSugar} x5 ]
 *   }}
 * 说明：当前/五时段指标为演示基线；接入穿戴设备后由 mock-api.js 运行时覆盖。
 */

(function(g){ if(typeof g==="undefined")return; g.RESIDENT_DATA = {
  "1": {
    "id": 1,
    "name": "王国栋",
    "gender": "男",
    "age": 72,
    "chronic": "高血压12年（控制良好）",
    "height": 170,
    "weight": 72,
    "posX": 150,
    "posY": 125,
    "place": "1号楼",
    "mobility": 0.7,
    "careLevel": "自理",
    "heartRate": 72,
    "bloodOxygen": 98,
    "temperature": 36.5,
    "systolic": 132,
    "diastolic": 82,
    "bloodSugar": 5.4,
    "medicalHistory": {
      "pastHistory": "原发性高血压12年，规律服药，血压控制可。高脂血症5年，饮食控制。",
      "allergies": "无",
      "medications": "氨氯地平 5mg qd（2020年起），阿托伐他汀 10mg qn（2021年起）",
      "surgicalHistory": "无",
      "familyHistory": "父亲：高血压、脑卒中（72岁发病）；母亲：体健。",
      "smoking": "已戒20年，既往吸烟30年，每日1包",
      "alcohol": "偶尔小酌，每周约2两白酒",
      "bloodType": "B",
      "lastCheckup": "2026-03-10，血压135/84，血脂控制可，心电图正常。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 73,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 135,
        "diastolic": 84,
        "bloodSugar": 5.2
      },
      {
        "name": "上午",
        "heartRate": 74,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 133,
        "diastolic": 83,
        "bloodSugar": 5.4
      },
      {
        "name": "中午",
        "heartRate": 72,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 133,
        "diastolic": 83,
        "bloodSugar": 5.9
      },
      {
        "name": "下午",
        "heartRate": 72,
        "bloodOxygen": 97.9,
        "temperature": 36.5,
        "systolic": 132,
        "diastolic": 82,
        "bloodSugar": 5.3
      },
      {
        "name": "晚间",
        "heartRate": 71,
        "bloodOxygen": 98,
        "temperature": 36.7,
        "systolic": 131,
        "diastolic": 82,
        "bloodSugar": 5.3
      }
    ]
  },
  "2": {
    "id": 2,
    "name": "张德福",
    "gender": "男",
    "age": 68,
    "chronic": "2型糖尿病8年（控制良好）",
    "height": 175,
    "weight": 80,
    "posX": 330,
    "posY": 125,
    "place": "2号楼",
    "mobility": 0.8,
    "careLevel": "自理",
    "heartRate": 78,
    "bloodOxygen": 97,
    "temperature": 36.3,
    "systolic": 132,
    "diastolic": 82,
    "bloodSugar": 5.4,
    "medicalHistory": {
      "pastHistory": "2型糖尿病8年，口服降糖药控制良好，糖化血红蛋白6.5%。",
      "allergies": "磺胺类药物（皮疹）",
      "medications": "二甲双胍 500mg bid（2018年起），格列美脲 2mg qd（2022年起）",
      "surgicalHistory": "无",
      "familyHistory": "母亲：2型糖尿病（65岁发病）；父亲：体健。",
      "smoking": "从不吸烟",
      "alcohol": "不饮酒",
      "bloodType": "O",
      "lastCheckup": "2026-04-22，空腹血糖6.1mmol/L，肾功能正常，眼底检查未见异常。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 79,
        "bloodOxygen": 97,
        "temperature": 36.3,
        "systolic": 135,
        "diastolic": 84,
        "bloodSugar": 5.2
      },
      {
        "name": "上午",
        "heartRate": 80,
        "bloodOxygen": 97,
        "temperature": 36.3,
        "systolic": 133,
        "diastolic": 83,
        "bloodSugar": 5.4
      },
      {
        "name": "中午",
        "heartRate": 78,
        "bloodOxygen": 97,
        "temperature": 36.3,
        "systolic": 133,
        "diastolic": 83,
        "bloodSugar": 5.9
      },
      {
        "name": "下午",
        "heartRate": 78,
        "bloodOxygen": 96.9,
        "temperature": 36.3,
        "systolic": 132,
        "diastolic": 82,
        "bloodSugar": 5.3
      },
      {
        "name": "晚间",
        "heartRate": 77,
        "bloodOxygen": 97,
        "temperature": 36.5,
        "systolic": 131,
        "diastolic": 82,
        "bloodSugar": 5.3
      }
    ]
  },
  "3": {
    "id": 3,
    "name": "李秀兰",
    "gender": "女",
    "age": 75,
    "chronic": "冠心病，高血压15年（血压偏高）",
    "height": 158,
    "weight": 62,
    "posX": 150,
    "posY": 265,
    "place": "3号楼",
    "mobility": 0.5,
    "careLevel": "介助",
    "heartRate": 86,
    "bloodOxygen": 95,
    "temperature": 36.7,
    "systolic": 148,
    "diastolic": 82,
    "bloodSugar": 5.9,
    "medicalHistory": {
      "pastHistory": "冠心病（稳定型心绞痛），高血压15年，最高血压170/95。2021年因不稳定心绞痛行冠脉造影+支架植入术（LAD中段，药物洗脱支架1枚）。术后规律服药，偶有劳力性胸闷。",
      "allergies": "无",
      "medications": "阿司匹林 100mg qd（2011年起），美托洛尔 25mg bid（2016年起），单硝酸异山梨酯 20mg bid（2021年起），氨氯地平 5mg qd",
      "surgicalHistory": "冠脉支架植入术（2021年3月，市第一中心医院，LAD中段DES×1）",
      "familyHistory": "父亲：冠心病、心肌梗死（68岁去世）；母亲：高血压。",
      "smoking": "从不吸烟",
      "alcohol": "从不饮酒",
      "bloodType": "A",
      "lastCheckup": "2026-05-08，血压148/82（略高），心电图示ST-T改变，心脏超声EF 58%。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 87,
        "bloodOxygen": 95,
        "temperature": 36.7,
        "systolic": 151,
        "diastolic": 84,
        "bloodSugar": 5.7
      },
      {
        "name": "上午",
        "heartRate": 88,
        "bloodOxygen": 95,
        "temperature": 36.7,
        "systolic": 149,
        "diastolic": 83,
        "bloodSugar": 5.9
      },
      {
        "name": "中午",
        "heartRate": 86,
        "bloodOxygen": 95,
        "temperature": 36.7,
        "systolic": 149,
        "diastolic": 83,
        "bloodSugar": 6.4
      },
      {
        "name": "下午",
        "heartRate": 86,
        "bloodOxygen": 94.9,
        "temperature": 36.7,
        "systolic": 148,
        "diastolic": 82,
        "bloodSugar": 5.8
      },
      {
        "name": "晚间",
        "heartRate": 85,
        "bloodOxygen": 95,
        "temperature": 36.9,
        "systolic": 147,
        "diastolic": 82,
        "bloodSugar": 5.8
      }
    ]
  },
  "4": {
    "id": 4,
    "name": "赵桂英",
    "gender": "女",
    "age": 81,
    "chronic": "高血压，骨质疏松（控制良好）",
    "height": 155,
    "weight": 52,
    "posX": 330,
    "posY": 125,
    "place": "2号楼",
    "mobility": 0.3,
    "careLevel": "介护",
    "heartRate": 66,
    "bloodOxygen": 97,
    "temperature": 36.1,
    "systolic": 136,
    "diastolic": 76,
    "bloodSugar": 5.2,
    "medicalHistory": {
      "pastHistory": "高血压10年，规律服药控制良好。骨质疏松，2019年因跌倒致左股骨颈骨折行髋关节置换术，术后恢复良好，可独立行走。",
      "allergies": "头孢菌素类（荨麻疹，2020年发现）",
      "medications": "厄贝沙坦 150mg qd（2016年起），钙尔奇D 600mg qd（2019年起），阿仑膦酸钠 70mg qw（2020年起）",
      "surgicalHistory": "左髋关节置换术（2019年6月，骨科医院，生物型假体，术后2周下地，3个月恢复独立行走）",
      "familyHistory": "母亲：骨质疏松、髋部骨折（84岁）；父亲：体健。",
      "smoking": "从不吸烟",
      "alcohol": "从不饮酒",
      "bloodType": "AB",
      "lastCheckup": "2026-02-15，血压136/76，骨密度T值-2.1（较前改善），假体位置良好。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 67,
        "bloodOxygen": 97,
        "temperature": 36.1,
        "systolic": 139,
        "diastolic": 78,
        "bloodSugar": 5
      },
      {
        "name": "上午",
        "heartRate": 68,
        "bloodOxygen": 97,
        "temperature": 36.1,
        "systolic": 137,
        "diastolic": 77,
        "bloodSugar": 5.2
      },
      {
        "name": "中午",
        "heartRate": 66,
        "bloodOxygen": 97,
        "temperature": 36.1,
        "systolic": 137,
        "diastolic": 77,
        "bloodSugar": 5.7
      },
      {
        "name": "下午",
        "heartRate": 66,
        "bloodOxygen": 96.9,
        "temperature": 36.1,
        "systolic": 136,
        "diastolic": 76,
        "bloodSugar": 5.1
      },
      {
        "name": "晚间",
        "heartRate": 65,
        "bloodOxygen": 97,
        "temperature": 36.3,
        "systolic": 135,
        "diastolic": 76,
        "bloodSugar": 5.1
      }
    ]
  },
  "5": {
    "id": 5,
    "name": "刘建军",
    "gender": "男",
    "age": 70,
    "chronic": "慢性支气管炎，轻度肺气肿（血氧偏低）",
    "height": 172,
    "weight": 68,
    "posX": 150,
    "posY": 265,
    "place": "3号楼",
    "mobility": 0.35,
    "careLevel": "介助",
    "heartRate": 88,
    "bloodOxygen": 93,
    "temperature": 36.8,
    "systolic": 128,
    "diastolic": 82,
    "bloodSugar": 5.5,
    "medicalHistory": {
      "pastHistory": "慢性支气管炎15年，每年冬季急性发作1-2次。肺功能示轻度阻塞性通气功能障碍（FEV1/FVC 65%），长期家庭氧疗（偶尔使用）。",
      "allergies": "无",
      "medications": "沙美特罗替卡松吸入剂 50/250μg 1吸 bid（2019年起），噻托溴铵吸入剂 18μg qd（2022年起），按需使用沙丁胺醇",
      "surgicalHistory": "无",
      "familyHistory": "父亲：慢性阻塞性肺疾病（COPD），肺心病（78岁去世）。",
      "smoking": "已戒5年，既往吸烟40年，每日1.5包",
      "alcohol": "偶尔饮用啤酒",
      "bloodType": "A",
      "lastCheckup": "2026-06-01，血氧饱和度静息93%，肺功能较前无明显变化，6分钟步行试验380m。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 89,
        "bloodOxygen": 93,
        "temperature": 36.8,
        "systolic": 131,
        "diastolic": 84,
        "bloodSugar": 5.3
      },
      {
        "name": "上午",
        "heartRate": 90,
        "bloodOxygen": 93,
        "temperature": 36.8,
        "systolic": 129,
        "diastolic": 83,
        "bloodSugar": 5.5
      },
      {
        "name": "中午",
        "heartRate": 88,
        "bloodOxygen": 93,
        "temperature": 36.8,
        "systolic": 129,
        "diastolic": 83,
        "bloodSugar": 6
      },
      {
        "name": "下午",
        "heartRate": 88,
        "bloodOxygen": 92.9,
        "temperature": 36.8,
        "systolic": 128,
        "diastolic": 82,
        "bloodSugar": 5.4
      },
      {
        "name": "晚间",
        "heartRate": 87,
        "bloodOxygen": 93,
        "temperature": 37,
        "systolic": 127,
        "diastolic": 82,
        "bloodSugar": 5.4
      }
    ]
  },
  "6": {
    "id": 6,
    "name": "陈美华",
    "gender": "女",
    "age": 77,
    "chronic": "2型糖尿病6年（控制良好）",
    "height": 160,
    "weight": 58,
    "posX": 610,
    "posY": 125,
    "place": "5号楼",
    "mobility": 0.6,
    "careLevel": "自理",
    "heartRate": 72,
    "bloodOxygen": 97,
    "temperature": 36.4,
    "systolic": 126,
    "diastolic": 78,
    "bloodSugar": 5.4,
    "medicalHistory": {
      "pastHistory": "2型糖尿病6年，口服降糖药控制良好。高脂血症，饮食控制。",
      "allergies": "青霉素（过敏性休克，1975年发现）",
      "medications": "二甲双胍 500mg bid（2020年起），瑞舒伐他汀 5mg qn（2021年起）",
      "surgicalHistory": "阑尾切除术（1995年）；白内障超声乳化+人工晶体植入术（2023年，双眼）",
      "familyHistory": "父亲：2型糖尿病、冠心病；母亲：2型糖尿病。",
      "smoking": "从不吸烟",
      "alcohol": "从不饮酒",
      "bloodType": "O",
      "lastCheckup": "2026-05-30，空腹血糖6.0mmol/L，HbA1c 6.4%，血脂正常，眼底未见糖尿病视网膜病变。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 73,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 129,
        "diastolic": 80,
        "bloodSugar": 5.2
      },
      {
        "name": "上午",
        "heartRate": 74,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 127,
        "diastolic": 79,
        "bloodSugar": 5.4
      },
      {
        "name": "中午",
        "heartRate": 72,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 127,
        "diastolic": 79,
        "bloodSugar": 5.9
      },
      {
        "name": "下午",
        "heartRate": 72,
        "bloodOxygen": 96.9,
        "temperature": 36.4,
        "systolic": 126,
        "diastolic": 78,
        "bloodSugar": 5.3
      },
      {
        "name": "晚间",
        "heartRate": 71,
        "bloodOxygen": 97,
        "temperature": 36.6,
        "systolic": 125,
        "diastolic": 78,
        "bloodSugar": 5.3
      }
    ]
  },
  "7": {
    "id": 7,
    "name": "周文博",
    "gender": "男",
    "age": 69,
    "chronic": "高血压5年，痛风（控制良好）",
    "height": 178,
    "weight": 82,
    "posX": 610,
    "posY": 125,
    "place": "5号楼",
    "mobility": 0.7,
    "careLevel": "自理",
    "heartRate": 74,
    "bloodOxygen": 98,
    "temperature": 36.6,
    "systolic": 134,
    "diastolic": 80,
    "bloodSugar": 5.3,
    "medicalHistory": {
      "pastHistory": "高血压5年，规律服药控制良好。痛风3年，每年发作1-2次（右足第一跖趾关节），饮食控制+药物预防。",
      "allergies": "无",
      "medications": "氯沙坦 50mg qd（2021年起），非布司他 40mg qd（2023年起），秋水仙碱 0.5mg qd（发作时）",
      "surgicalHistory": "无",
      "familyHistory": "父亲：高血压、痛风；母亲：体健。",
      "smoking": "从不吸烟",
      "alcohol": "已戒酒3年，既往饮酒30年，每日约3两白酒",
      "bloodType": "B",
      "lastCheckup": "2026-04-18，血压134/80，尿酸386μmol/L（控制良好），肾功能正常。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 75,
        "bloodOxygen": 98,
        "temperature": 36.6,
        "systolic": 137,
        "diastolic": 82,
        "bloodSugar": 5.1
      },
      {
        "name": "上午",
        "heartRate": 76,
        "bloodOxygen": 98,
        "temperature": 36.6,
        "systolic": 135,
        "diastolic": 81,
        "bloodSugar": 5.3
      },
      {
        "name": "中午",
        "heartRate": 74,
        "bloodOxygen": 98,
        "temperature": 36.6,
        "systolic": 135,
        "diastolic": 81,
        "bloodSugar": 5.8
      },
      {
        "name": "下午",
        "heartRate": 74,
        "bloodOxygen": 97.9,
        "temperature": 36.6,
        "systolic": 134,
        "diastolic": 80,
        "bloodSugar": 5.2
      },
      {
        "name": "晚间",
        "heartRate": 73,
        "bloodOxygen": 98,
        "temperature": 36.8,
        "systolic": 133,
        "diastolic": 80,
        "bloodSugar": 5.2
      }
    ]
  },
  "8": {
    "id": 8,
    "name": "吴玉兰",
    "gender": "女",
    "age": 79,
    "chronic": "冠心病，2型糖尿病10年（控制欠佳）",
    "height": 156,
    "weight": 60,
    "posX": 610,
    "posY": 265,
    "place": "6号楼",
    "mobility": 0.4,
    "careLevel": "介助",
    "heartRate": 84,
    "bloodOxygen": 96,
    "temperature": 36.8,
    "systolic": 150,
    "diastolic": 90,
    "bloodSugar": 7.2,
    "medicalHistory": {
      "pastHistory": "冠心病合并2型糖尿病10年。2018年因急性冠脉综合征行急诊PCI（RCA中段，DES×1），2023年复查冠脉造影示支架内再狭窄50%，行球囊扩张术。糖尿病病程10年，口服药+胰岛素控制，血糖波动较大。",
      "allergies": "碘造影剂（轻度皮疹，2023年发现）",
      "medications": "阿司匹林 100mg qd（2016年起），替格瑞洛 90mg bid（2023年起），二甲双胍 500mg bid（2016年起），甘精胰岛素 8U qn（2021年起），阿托伐他汀 20mg qn",
      "surgicalHistory": "冠脉支架植入术×2（2018年7月 RCA近段DES×1；2023年1月 球囊扩张术）；白内障手术（2020年，右眼）",
      "familyHistory": "父亲：冠心病、急性心肌梗死（65岁去世）；母亲：2型糖尿病。",
      "smoking": "从不吸烟",
      "alcohol": "从不饮酒",
      "bloodType": "A",
      "lastCheckup": "2026-06-12，血压150/90（偏高），空腹血糖7.3mmol/L，HbA1c 7.8%，建议调整胰岛素剂量。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 85,
        "bloodOxygen": 96,
        "temperature": 36.8,
        "systolic": 153,
        "diastolic": 92,
        "bloodSugar": 7
      },
      {
        "name": "上午",
        "heartRate": 86,
        "bloodOxygen": 96,
        "temperature": 36.8,
        "systolic": 151,
        "diastolic": 91,
        "bloodSugar": 7.2
      },
      {
        "name": "中午",
        "heartRate": 84,
        "bloodOxygen": 96,
        "temperature": 36.8,
        "systolic": 151,
        "diastolic": 91,
        "bloodSugar": 7.7
      },
      {
        "name": "下午",
        "heartRate": 84,
        "bloodOxygen": 95.9,
        "temperature": 36.8,
        "systolic": 150,
        "diastolic": 90,
        "bloodSugar": 7.1
      },
      {
        "name": "晚间",
        "heartRate": 83,
        "bloodOxygen": 96,
        "temperature": 37,
        "systolic": 149,
        "diastolic": 90,
        "bloodSugar": 7.1
      }
    ]
  },
  "9": {
    "id": 9,
    "name": "郑国华",
    "gender": "男",
    "age": 73,
    "chronic": "良性前列腺增生，高血压8年（控制良好）",
    "height": 174,
    "weight": 75,
    "posX": 610,
    "posY": 265,
    "place": "6号楼",
    "mobility": 0.6,
    "careLevel": "自理",
    "heartRate": 68,
    "bloodOxygen": 97,
    "temperature": 36.2,
    "systolic": 132,
    "diastolic": 80,
    "bloodSugar": 5.1,
    "medicalHistory": {
      "pastHistory": "良性前列腺增生5年，夜尿2-3次。高血压8年，规律服药控制良好。",
      "allergies": "无",
      "medications": "坦索罗辛 0.2mg qn（2021年起），非那雄胺 5mg qd（2021年起），氨氯地平 5mg qd（2018年起）",
      "surgicalHistory": "无",
      "familyHistory": "父亲：前列腺增生、前列腺癌（78岁诊断）；母亲：体健。",
      "smoking": "已戒15年，既往吸烟25年，每日半包",
      "alcohol": "不饮酒",
      "bloodType": "B",
      "lastCheckup": "2026-03-28，血压132/80，PSA 2.1ng/mL（正常），前列腺B超示中度增生。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 69,
        "bloodOxygen": 97,
        "temperature": 36.2,
        "systolic": 135,
        "diastolic": 82,
        "bloodSugar": 4.9
      },
      {
        "name": "上午",
        "heartRate": 70,
        "bloodOxygen": 97,
        "temperature": 36.2,
        "systolic": 133,
        "diastolic": 81,
        "bloodSugar": 5.1
      },
      {
        "name": "中午",
        "heartRate": 68,
        "bloodOxygen": 97,
        "temperature": 36.2,
        "systolic": 133,
        "diastolic": 81,
        "bloodSugar": 5.6
      },
      {
        "name": "下午",
        "heartRate": 68,
        "bloodOxygen": 96.9,
        "temperature": 36.2,
        "systolic": 132,
        "diastolic": 80,
        "bloodSugar": 5
      },
      {
        "name": "晚间",
        "heartRate": 67,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 131,
        "diastolic": 80,
        "bloodSugar": 5
      }
    ]
  },
  "10": {
    "id": 10,
    "name": "孙淑珍",
    "gender": "女",
    "age": 76,
    "chronic": "2型糖尿病5年，骨质疏松（控制良好）",
    "height": 162,
    "weight": 55,
    "posX": 150,
    "posY": 450,
    "place": "7号楼",
    "mobility": 0.5,
    "careLevel": "自理",
    "heartRate": 78,
    "bloodOxygen": 98,
    "temperature": 36.5,
    "systolic": 130,
    "diastolic": 82,
    "bloodSugar": 5.4,
    "medicalHistory": {
      "pastHistory": "2型糖尿病5年，口服药控制良好。骨质疏松，骨密度T值-2.5（腰椎），规律用药+补钙。",
      "allergies": "无",
      "medications": "二甲双胍 500mg bid（2021年起），阿仑膦酸钠 70mg qw（2022年起），钙尔奇D 600mg qd",
      "surgicalHistory": "白内障超声乳化+人工晶体植入术（2022年，左眼）",
      "familyHistory": "母亲：2型糖尿病、骨质疏松、腰椎压缩性骨折（80岁）；父亲：体健。",
      "smoking": "从不吸烟",
      "alcohol": "从不饮酒",
      "bloodType": "O",
      "lastCheckup": "2026-05-15，空腹血糖6.0mmol/L，骨密度T值-2.3（较前改善），眼科检查正常。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 79,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 133,
        "diastolic": 84,
        "bloodSugar": 5.2
      },
      {
        "name": "上午",
        "heartRate": 80,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 131,
        "diastolic": 83,
        "bloodSugar": 5.4
      },
      {
        "name": "中午",
        "heartRate": 78,
        "bloodOxygen": 98,
        "temperature": 36.5,
        "systolic": 131,
        "diastolic": 83,
        "bloodSugar": 5.9
      },
      {
        "name": "下午",
        "heartRate": 78,
        "bloodOxygen": 97.9,
        "temperature": 36.5,
        "systolic": 130,
        "diastolic": 82,
        "bloodSugar": 5.3
      },
      {
        "name": "晚间",
        "heartRate": 77,
        "bloodOxygen": 98,
        "temperature": 36.7,
        "systolic": 129,
        "diastolic": 82,
        "bloodSugar": 5.3
      }
    ]
  },
  "11": {
    "id": 11,
    "name": "黄志强",
    "gender": "男",
    "age": 71,
    "chronic": "慢性胃炎，高血压3年（控制良好）",
    "height": 176,
    "weight": 70,
    "posX": 150,
    "posY": 450,
    "place": "7号楼",
    "mobility": 0.7,
    "careLevel": "自理",
    "heartRate": 72,
    "bloodOxygen": 97,
    "temperature": 36.4,
    "systolic": 134,
    "diastolic": 80,
    "bloodSugar": 5.2,
    "medicalHistory": {
      "pastHistory": "慢性萎缩性胃炎5年，偶有上腹不适，胃镜示轻度萎缩（2024年），HP阴性。高血压3年，低剂量单药控制良好。",
      "allergies": "无",
      "medications": "奥美拉唑 20mg qd（按需），氯沙坦 50mg qd（2023年起），叶酸 5mg qd",
      "surgicalHistory": "无",
      "familyHistory": "父亲：胃癌（75岁诊断，胃大部切除术后存活）；母亲：体健。",
      "smoking": "从不吸烟",
      "alcohol": "已戒酒10年，既往饮酒20年，每日约2两白酒",
      "bloodType": "AB",
      "lastCheckup": "2026-04-05，血压134/80，胃镜复查：轻度萎缩，未见异型增生，建议2年后复查。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 73,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 137,
        "diastolic": 82,
        "bloodSugar": 5
      },
      {
        "name": "上午",
        "heartRate": 74,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 135,
        "diastolic": 81,
        "bloodSugar": 5.2
      },
      {
        "name": "中午",
        "heartRate": 72,
        "bloodOxygen": 97,
        "temperature": 36.4,
        "systolic": 135,
        "diastolic": 81,
        "bloodSugar": 5.7
      },
      {
        "name": "下午",
        "heartRate": 72,
        "bloodOxygen": 96.9,
        "temperature": 36.4,
        "systolic": 134,
        "diastolic": 80,
        "bloodSugar": 5.1
      },
      {
        "name": "晚间",
        "heartRate": 71,
        "bloodOxygen": 97,
        "temperature": 36.6,
        "systolic": 133,
        "diastolic": 80,
        "bloodSugar": 5.1
      }
    ]
  },
  "12": {
    "id": 12,
    "name": "林秀英",
    "gender": "女",
    "age": 80,
    "chronic": "脑梗后遗症，高血压20年，糖尿病12年（控制欠佳）",
    "height": 154,
    "weight": 56,
    "posX": 400,
    "posY": 552,
    "place": "社区大门",
    "mobility": 0.2,
    "careLevel": "介护",
    "heartRate": 78,
    "bloodOxygen": 95,
    "temperature": 37,
    "systolic": 152,
    "diastolic": 86,
    "bloodSugar": 7.3,
    "medicalHistory": {
      "pastHistory": "2024年3月突发右侧肢体无力，头颅CT+MRI示左侧基底节区急性脑梗死，NIHSS评分8分。经溶栓+康复治疗，遗留右侧肢体轻度偏瘫（肌力4级），可扶拐行走。高血压20年、2型糖尿病12年，血压血糖控制欠佳，为本次脑梗的主要危险因素。",
      "allergies": "碘造影剂（过敏性休克，2024年发现，绝对禁忌）",
      "medications": "阿司匹林 100mg qd（2006年起），氯吡格雷 75mg qd（2024年起），氨氯地平 5mg qd，瑞舒伐他汀 10mg qn，甘精胰岛素 10U qn（2024年调量），二甲双胍 500mg bid",
      "surgicalHistory": "无",
      "familyHistory": "父亲：高血压、脑梗死（78岁发病）；母亲：高血压、脑出血（82岁去世）；弟弟：高血压、2型糖尿病。",
      "smoking": "从不吸烟（丈夫吸烟，二手烟暴露40年）",
      "alcohol": "从不饮酒",
      "bloodType": "A",
      "lastCheckup": "2026-06-28，血压152/86（高），空腹血糖7.5mmol/L，HbA1c 8.1%，血脂偏高，康复评定：右侧肌力4级，Barthel指数75分。"
    },
    "slots": [
      {
        "name": "清晨",
        "heartRate": 79,
        "bloodOxygen": 95,
        "temperature": 37,
        "systolic": 155,
        "diastolic": 88,
        "bloodSugar": 7.1
      },
      {
        "name": "上午",
        "heartRate": 80,
        "bloodOxygen": 95,
        "temperature": 37,
        "systolic": 153,
        "diastolic": 87,
        "bloodSugar": 7.3
      },
      {
        "name": "中午",
        "heartRate": 78,
        "bloodOxygen": 95,
        "temperature": 37,
        "systolic": 153,
        "diastolic": 87,
        "bloodSugar": 7.8
      },
      {
        "name": "下午",
        "heartRate": 78,
        "bloodOxygen": 94.9,
        "temperature": 37,
        "systolic": 152,
        "diastolic": 86,
        "bloodSugar": 7.2
      },
      {
        "name": "晚间",
        "heartRate": 77,
        "bloodOxygen": 95,
        "temperature": 37.2,
        "systolic": 151,
        "diastolic": 86,
        "bloodSugar": 7.2
      }
    ]
  }
};
 if(typeof module!=="undefined"&&module.exports){module.exports=g.RESIDENT_DATA;} })(typeof window!=="undefined"?window:globalThis);
