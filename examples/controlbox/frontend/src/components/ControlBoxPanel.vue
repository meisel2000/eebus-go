<template>
  <h1>Control Box Simulator</h1>
  <div class="header-frame">
    <div class="header" v-if="'' < qrcode">
      <label class="qrcode-text">{{ qrcode }}</label>
      <qrcode-vue  class="qrcode" :value="qrcode" :size="130" level="H" render-as="svg" />
    </div>
  </div>
  <div v-if="'' == qrcode">
    <h3>not running</h3>
  </div>
  <div v-else>
    <div v-if="0 < remoteEntities.length" class="devices">
      <label class="device-select-label">Connected Device:</label>
      <VueSelect v-model="ski" :options="optionEntities"
        placeholder="Select a connected device" @option-selected="deviceSelected">
      </VueSelect>
      <label class="device-select-label">SKI:</label>
      <label class="device-select-label">{{ ski }}</label>
    </div>
    <div class="usecases">
      <div v-if="'' < ski">
        <h3>Consumption Limit</h3>
        <div class="form-line">
          <label>Active:</label>
          <input type="checkbox" v-model="dd[ski]['LPC'].IsActive"/>

          <label>Dimmed Value [W]:</label>
          <input type="number" v-model="dd[ski]['LPC'].Value" />
          <button class="three-lines" type="button" @click="setConsumptionLimit">Set</button>

          <label>Dimmed Duration [s]:</label>
          <input type="number" v-model="dd[ski]['LPC'].Duration" />

          <label>Failsafe Value [W]:</label>
          <input type="number" v-model="dd[ski]['LPC'].FSValue" />
          <button type="button" @click="setConsumptionFailsafeLimit">Set</button>

          <label>Failsafe Duration [s]:</label>
          <input type="number" v-model="dd[ski]['LPC'].FSDuration" />
          <button type="button" @click="setConsumptionFailsafeDuration">Set</button>

          <label>Nominal Maximum [W]:</label>
          <input type="number" v-model="consumptionNominalMax" />
          <div></div>
          <!-- <button type="button" @click="getConsumptionNominalMax">Get</button> -->

          <label>Heartbeat:</label>
          <span v-bind:class = "(consumptionHeartbeat)?'pulse heartbeat':'pulse'">&#9673;</span>
          <!-- <button type="button" @click="toggleConsumptionHeartbeat">{{ consumptionHeartbeatEnabled ? 'Stop' : 'Start' }}</button> -->
          <div></div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
  import { Component, Vue, toNative } from 'vue-facing-decorator'
  import QrcodeVue from 'qrcode.vue'
  import VueSelect from 'vue3-select-component'

  enum MessageType {
    Text                           = 0,
    QRCode                         = 1,
    Acknowledge                    = 2,
    EntityListChanged              = 3,
    GetEntityList                  = 4,
    GetAllData                     = 5,
    SetConsumptionLimit            = 6,
    GetConsumptionLimit            = 7,
    SetProductionLimit             = 8,
    GetProductionLimit             = 9,
    SetConsumptionFailsafeValue    = 10,
    GetConsumptionFailsafeValue    = 11,
    SetConsumptionFailsafeDuration = 12,
    GetConsumptionFailsafeDuration = 13,
    SetProductionFailsafeValue     = 14,
    GetProductionFailsafeValue     = 15,
    SetProductionFailsafeDuration  = 16,
    GetProductionFailsafeDuration  = 17,
    GetConsumptionNominalMax       = 18,
    GetProductionNominalMax        = 19,
    GetConsumptionHeartbeat        = 20,
    StopConsumptionHeartbeat       = 21,
    StartConsumptionHeartbeat      = 22,
    GetProductionHeartbeat         = 23,
    StopProductionHeartbeat        = 24,
    StartProductionHeartbeat       = 25
  }

  interface Limits {
    IsActive:   boolean,
	  Value:      number,
	  Duration:   number,
	  FSValue:    number,
	  FSDuration: number
  }

  interface EntityDescription {
    Name:     string,
    SKI:      string,
    UseCases: string[]
  }

  interface Message {
    Type:        MessageType,
    Text?:       string,
    Limit?:      Limits,
    Value?:      number,
    EntityList?: EntityDescription[],
    UseCase?:    string
  }

  type UCLimits = {[key:string]:Limits};
  type DeviceData = {[key:string]:UCLimits};

  @Component({
    components: {
      QrcodeVue,
      VueSelect
    }
  })
  export class ControlBoxPanel extends Vue {
    public qrcode = "";

    public remoteEntities: EntityDescription[] = [];
    public ski = "";

    public get optionEntities() {
      var options:any[] = [];
      this.remoteEntities.forEach(item => { options.push({
          label: item.Name,
          value: item.SKI
        });        
      });
      return options;
    }

    public dd: DeviceData = {};

    public consumptionNominalMax: number = 0;
    public productionNominalMax:  number = 0;

    public consumptionHeartbeat:        boolean = false;
    public consumptionHeartbeatEnabled: boolean = true;
    public productionHeartbeat:         boolean = false;
    public productionHeartbeatEnabled:  boolean = true;

    private socket: WebSocket | undefined;
  
    mounted() {
      this.socket = new WebSocket( "ws://localhost:7070/ws" );
      console.log( "Attempting Connection..." );

      this.socket.onopen = () => {
          console.log( "Successfully Connected" );
          //this.sendText( "Hi From the Client!" );
          this.sendNotification( MessageType.GetEntityList );
      };
      
      this.socket.onclose = event => {
          console.log( "Socket Closed Connection: ", event );
          this.sendText( "Client Closed!" );
          this.socket = undefined;
      };

      this.socket.onerror = error => {
          console.log( "Socket Error: ", error );
      };

      this.socket.onmessage = event => {
        console.log( "Socket message: ", event.data );
        var message: Message = JSON.parse( event.data );
        if ( message.Type == MessageType.QRCode ) {
          this.qrcode = message.Text as string;
        }
        else if ( message.Type == MessageType.EntityListChanged ) {
          this.sendNotification( MessageType.GetEntityList );
        }
        else if ( message.Type == MessageType.GetEntityList ) {
          this.remoteEntities = message.EntityList!;
          this.updateDeviceData();
          if ( ! this.ski && 0 < this.remoteEntities.length )
            this.ski = this.remoteEntities[0].SKI;
          this.sendNotification( MessageType.GetAllData )
        }
        else if ( message.Type == MessageType.GetConsumptionLimit ) {
          this.dd[this.ski][message.UseCase!].IsActive = message.Limit?.IsActive ?? false;
          this.dd[this.ski][message.UseCase!].Value    = message.Limit?.Value ?? 0;
          this.dd[this.ski][message.UseCase!].Duration = message.Limit?.Duration ?? 0;
        }
        else if ( message.Type == MessageType.GetConsumptionFailsafeValue ) {
          this.dd[this.ski]['LPC'].FSValue = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetConsumptionFailsafeDuration ) {
          this.dd[this.ski]['LPC'].FSDuration = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetProductionLimit ) {
          this.dd[this.ski][message.UseCase!].IsActive = message.Limit?.IsActive ?? false;
          this.dd[this.ski][message.UseCase!].Value    = message.Limit?.Value ?? 0;
          this.dd[this.ski][message.UseCase!].Duration = message.Limit?.Duration ?? 0;
        }
        else if ( message.Type == MessageType.GetProductionFailsafeValue ) {
          this.dd[this.ski]['LPP'].FSValue = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetProductionFailsafeDuration ) {
          this.dd[this.ski]['LPP'].FSDuration = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetConsumptionNominalMax ) {
          this.consumptionNominalMax = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetProductionNominalMax ) {
          this.productionNominalMax = message.Value ?? 0;
        }
        else if ( message.Type == MessageType.GetConsumptionHeartbeat ) {
          this.consumptionHeartbeat = false;
          setTimeout( () => this.consumptionHeartbeat = true, 10 );
        }
        else if ( message.Type == MessageType.GetProductionHeartbeat ) {
          this.productionHeartbeat = false;
          setTimeout( () => this.productionHeartbeat = true, 10 );
        }
      }
    }

    private updateDeviceData() {
      if ( ! this.remoteEntities ) {
        this.dd = {};
        return;
      }

      this.remoteEntities.forEach( re => {
        if ( -1 == Object.keys( this.dd ).findIndex( ski => ski == re.SKI ) ) {
          this.dd[re.SKI] = {};
        }

        var dd = this.dd[re.SKI];
        re.UseCases.forEach( reuc => {
          if ( -1 == Object.keys( dd ).findIndex( uc => uc == reuc ) ) {
            dd[reuc] = {} as Limits;
          }
        });
      });
    }

    public deviceSelected() {
      this.sendNotification( MessageType.GetAllData );
    }

    private sendNotification( type: MessageType ) {
      let command: Message = {
        Type: type,
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendText( text: string ) {
      let command: Message = {
        Type: MessageType.Text,
        Text: text,
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendLimits( type: MessageType, value: Limits ) {
      let command: Message = {
        Type:  type,
        Limit: value
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    private sendValue( type: MessageType, value: number ) {
      let command: Message = {
        Type:  type,
        Value: value
      };

      this.socket!.send( JSON.stringify( command ) );
    }

    public setConsumptionLimit() {
      if ( ! this.socket )
        return;
      
      this.sendLimits( MessageType.SetConsumptionLimit, this.dd[this.ski]['LPC'] );
    }

    public setProductionLimit() {
      if ( ! this.socket )
        return;
      
      this.sendLimits( MessageType.SetProductionLimit, this.dd[this.ski]['LPP'] );
    }

    public setConsumptionFailsafeLimit() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetConsumptionFailsafeValue, this.dd[this.ski]['LPC'].FSValue );
    }

    public setConsumptionFailsafeDuration() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetConsumptionFailsafeDuration, this.dd[this.ski]['LPC'].FSDuration );
    }

    public setProductionFailsafeLimit() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetProductionFailsafeValue, this.dd[this.ski]['LPP'].FSValue );
    }

    public setProductionFailsafeDuration() {
      if ( ! this.socket )
        return;
      
      this.sendValue( MessageType.SetProductionFailsafeDuration, this.dd[this.ski]['LPP'].FSDuration );
    }

    // public getConsumptionNominalMax() {
    //   if ( ! this.socket )
    //     return;
      
    //   this.sendValue( MessageType.GetConsumptionNominalMax, 0 );
    // }

    public toggleConsumptionHeartbeat() {
      if ( ! this.socket )
        return;

      if ( this.consumptionHeartbeatEnabled )
        this.sendValue( MessageType.StopConsumptionHeartbeat, 0 );
      else
        this.sendValue( MessageType.StartConsumptionHeartbeat, 0 );

      this.consumptionHeartbeatEnabled = ! this.consumptionHeartbeatEnabled;
    }

    public toggleProductionHeartbeat() {
      if ( ! this.socket )
        return;

      if ( this.productionHeartbeatEnabled )
        this.sendValue( MessageType.StopProductionHeartbeat, 0 );
      else
        this.sendValue( MessageType.StartProductionHeartbeat, 0 );

      this.productionHeartbeatEnabled = ! this.productionHeartbeatEnabled;
    }
  }

  export default toNative( ControlBoxPanel )
</script>

<style scoped>
  .read-the-docs {
    color: #888;
  }
  .header-frame {
    display: inline-table;
    margin-bottom: 10px;
  }
  .header {
    display: grid;
    grid-template-columns: 75fr 25fr;
    column-gap: 25px;
  }
  h1 {
    margin-top: 0.1em;
  }
  .qrcode-text {
    width: 370px;
    text-align: left;
    word-break: break-all;
  }
  .qrcode {
    width: 130px;
    height: 100%;
  }
  .usecases {
    display: grid;
    grid-template-columns: 50fr 50fr;
    column-gap: 25px;
  }
  .three-lines {
    grid-column-start: 3;
    grid-row-start: 1;
    grid-row-end: 4;
  }
  .form-line {
    display: grid;
    grid-template-columns: 50fr 30fr 20fr;
    column-gap: 10px;
  }
  .form-line label {
    align-content: center;
    text-align: left;
  }
  .form-line input {
    font-size: initial;
    width: 100px;
    align-self: center;
  }
  .form-line button {
    line-height: 5px;
    height: 100%;
  }
  
  .pulse {
    font-size: 25px;
  }

  .heartbeat {
    animation-name: heartbeat;
    animation-duration: 1s;
    /* animation-iteration-count: infinite; */
  }

  @keyframes heartbeat {
    from {
      color: rgb(0,255,0);
    }

    25% {
      color: rgb(0,255,0);
    }

    50% {
      color: rgb(0,127,0);
    }

    75% {
      color: rgb(0,63,0);
    }

    to {
      color: rgb(0,0,0);
    }
  }

  .devices {
    display: grid;
    grid-template-columns: 20fr 80fr;
    column-gap: 10px;
  }
  .device-select-label {
    text-align: left;
    line-height: 2.2em;
  }
</style>
